package mtclient

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"

	"github.com/teamgram/proto/mtproto"
)

// What a client does with a CDN DC (https://core.telegram.org/cdn), written
// after the official clients so that the tests check the server against
// what they accept.

// CdnHashChunk is the piece of a file one hash of upload.fileCdnRedirect
// and upload.getCdnFileHashes covers: 128 KB of the decrypted file, at an
// offset divisible by it (Android cdnChunkCheckSize,
// FileLoadOperation.java:168, 1889-1897; tdesktop downloads 128 KB parts,
// kDownloadPartSize in download_manager_mtproto.h:26, and checks each
// against the hash at its offset, download_manager_mtproto.cpp:704-737).
const CdnHashChunk = 128 << 10

// CdnPublicKey returns the RSA key of CDN DC dc in help.getCdnConfig. The
// key must be PKCS#1 PEM: Android reads it with PEM_read_bio_RSAPublicKey,
// which takes no other form (TMessagesProj/jni/tgnet/Handshake.cpp:1032).
func CdnPublicKey(cfg *mtproto.CdnConfig, dc int32) (*rsa.PublicKey, error) {
	for _, k := range cfg.GetPublicKeys() {
		if k.GetDcId() != dc {
			continue
		}
		block, _ := pem.Decode([]byte(k.GetPublicKey()))
		if block == nil || block.Type != "RSA PUBLIC KEY" {
			return nil, fmt.Errorf("mtclient: the key of CDN DC %d is not a PKCS#1 PEM public key", dc)
		}
		return x509.ParsePKCS1PublicKey(block.Bytes)
	}
	return nil, fmt.Errorf("mtclient: help.getCdnConfig has no key of DC %d", dc)
}

// CdnRedirect is upload.fileCdnRedirect as the clients take it.
type CdnRedirect struct {
	DC        int32
	FileToken []byte
	Key       []byte // AES-256 key, 32 bytes
	IV        []byte // 16 bytes
	Hashes    []*mtproto.FileHash
}

// ParseCdnRedirect checks a redirect as the clients do: a key of 32 bytes
// and an IV of 16, or Android fails the download (FileLoadOperation.java:2602-2611),
// tdesktop cancels it (download_manager_mtproto.cpp:1018) and iOS reports
// CDN_REDIRECT_KEY_INVALID (FetchV2.swift:1002-1004).
func ParseCdnRedirect(f *mtproto.Upload_File) (*CdnRedirect, error) {
	if f.GetPredicateName() != mtproto.Predicate_upload_fileCdnRedirect {
		return nil, fmt.Errorf("mtclient: %s is not upload.fileCdnRedirect", f.GetPredicateName())
	}
	if len(f.GetEncryptionKey()) != 32 || len(f.GetEncryptionIv()) != 16 {
		return nil, fmt.Errorf("mtclient: a CDN redirect with a key of %d bytes and an IV of %d", len(f.GetEncryptionKey()), len(f.GetEncryptionIv()))
	}
	if f.GetDcId() == 0 || len(f.GetFileToken()) == 0 {
		return nil, errors.New("mtclient: a CDN redirect without a DC or a file token")
	}
	return &CdnRedirect{
		DC:        f.GetDcId(),
		FileToken: slices.Clone(f.GetFileToken()),
		Key:       slices.Clone(f.GetEncryptionKey()),
		IV:        slices.Clone(f.GetEncryptionIv()),
		Hashes:    f.GetFileHashes(),
	}, nil
}

// CdnDecrypt decrypts the bytes upload.getCdnFile returned for offset: AES-256-CTR
// with the IV of the redirect whose last 4 bytes are offset/16, big-endian
// (Android FileLoadOperation.java:1899-1906; tdesktop
// download_manager_mtproto.cpp:688-698; iOS FetchV2.swift:929-934).
func CdnDecrypt(key, iv []byte, offset int64, data []byte) ([]byte, error) {
	if len(key) != 32 || len(iv) != 16 {
		return nil, errors.New("mtclient: CdnDecrypt needs a key of 32 bytes and an IV of 16")
	}
	if offset < 0 || offset%16 != 0 || offset/16 > 1<<32-1 {
		return nil, fmt.Errorf("mtclient: CdnDecrypt at offset %d", offset)
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	ctr := slices.Clone(iv)
	binary.BigEndian.PutUint32(ctr[12:], uint32(offset/16))
	out := make([]byte, len(data))
	cipher.NewCTR(b, ctr).XORKeyStream(out, data)
	return out, nil
}

// CdnHashes are the hashes of a file the client has, by offset.
type CdnHashes struct {
	byOffset map[int64]*mtproto.FileHash
}

// NewCdnHashes returns the hashes of list.
func NewCdnHashes(list []*mtproto.FileHash) *CdnHashes {
	h := &CdnHashes{byOffset: map[int64]*mtproto.FileHash{}}
	h.Add(list)
	return h
}

// Add keeps the hashes of list, as the clients keep every hash they are
// given (Android FileLoadOperation.java:1829-1835).
func (h *CdnHashes) Add(list []*mtproto.FileHash) {
	for _, fh := range list {
		h.byOffset[hashOffset(fh)] = fh
	}
}

// Len is the number of hashes kept.
func (h *CdnHashes) Len() int { return len(h.byOffset) }

func hashOffset(fh *mtproto.FileHash) int64 {
	if fh.GetOffset_INT64() == 0 {
		// The constructor of the old layers carries an int.
		return int64(fh.GetOffset_INT32())
	}
	return fh.GetOffset_INT64()
}

// Missing returns the offset of the first piece of [offset, offset+n)
// without a hash, and false when every piece has one. The client asks
// upload.getCdnFileHashes for it (Android FileLoadOperation.java:1892-1896).
func (h *CdnHashes) Missing(offset int64, n int) (int64, bool) {
	for p := offset / CdnHashChunk * CdnHashChunk; p < offset+int64(n); p += CdnHashChunk {
		if h.byOffset[p] == nil {
			return p, true
		}
	}
	return 0, false
}

// Check checks the decrypted bytes plain of the file at offset, divisible
// by CdnHashChunk: each piece of CdnHashChunk bytes, and a shorter last
// one, must have a hash of its length and its SHA-256 (Android
// FileLoadOperation.java:2016-2045; tdesktop download_manager_mtproto.cpp:700-737).
func (h *CdnHashes) Check(offset int64, plain []byte) error {
	if offset%CdnHashChunk != 0 {
		return fmt.Errorf("mtclient: the hashes are checked from a multiple of %d, not %d", CdnHashChunk, offset)
	}
	for start := 0; start < len(plain); start += CdnHashChunk {
		piece := plain[start:min(start+CdnHashChunk, len(plain))]
		at := offset + int64(start)
		fh := h.byOffset[at]
		if fh == nil {
			return fmt.Errorf("mtclient: no hash of the piece at %d", at)
		}
		sum := sha256.Sum256(piece)
		if int(fh.GetLimit()) != len(piece) || !bytes.Equal(fh.GetHash(), sum[:]) {
			return fmt.Errorf("mtclient: the piece at %d (%d bytes) does not match its hash (limit %d)", at, len(piece), fh.GetLimit())
		}
	}
	return nil
}
