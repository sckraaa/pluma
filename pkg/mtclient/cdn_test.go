package mtclient_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/teamgram/proto/mtproto"

	"github.com/teamgram/teamgram-server/pkg/cdn"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// homeFile is a file as the home prepares it for a node: its secrets, the
// cipher and the hashes of the plaintext (pkg/cdn).
func homeFile(t *testing.T, size int) (plain, cipherText []byte, s cdn.Secrets, blob []byte) {
	t.Helper()
	plain = randomBytes(t, size)
	s, err := cdn.NewSecrets()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := cdn.NewEncrypter(s.Key, s.IV)
	if err != nil {
		t.Fatal(err)
	}
	cipherText = make([]byte, size)
	// In uneven writes, as the pusher reads parts.
	for off := 0; off < size; off += 300 << 10 {
		end := min(off+300<<10, size)
		enc.XORKeyStream(cipherText[off:end], plain[off:end])
	}
	h := cdn.NewHasher()
	_, _ = h.Write(plain)
	return plain, cipherText, s, h.Sum()
}

// TestCdnDownloadOfHomeFile: a file the home encrypted and hashed is
// downloaded as the clients do, by 128 KB and by 1 MB parts, decrypted at
// each offset and checked against the hashes of a redirect and of
// upload.getCdnFileHashes.
func TestCdnDownloadOfHomeFile(t *testing.T) {
	const size = 3<<20 + 17
	plain, cipherText, s, blob := homeFile(t, size)

	first, err := cdn.Answer(blob, size, 0)
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := mtclient.ParseCdnRedirect(mtproto.MakeTLUploadFileCdnRedirect(&mtproto.Upload_File{
		DcId: 201, FileToken: s.Token, EncryptionKey: s.Key, EncryptionIv: s.IV,
		// Two hashes: the others come as upload.getCdnFileHashes gives them.
		FileHashes: first[:2],
	}).To_Upload_File())
	if err != nil {
		t.Fatal(err)
	}

	for _, part := range []int{128 << 10, 1 << 20} {
		hashes := mtclient.NewCdnHashes(redirect.Hashes)
		var got []byte
		for off := 0; off < size; off += part {
			chunk := cipherText[off:min(off+part, size)]
			dec, err := mtclient.CdnDecrypt(redirect.Key, redirect.IV, int64(off), chunk)
			if err != nil {
				t.Fatal(err)
			}
			for {
				at, missing := hashes.Missing(int64(off), len(dec))
				if !missing {
					break
				}
				more, err := cdn.Answer(blob, size, at)
				if err != nil || len(more) == 0 {
					t.Fatalf("hashes from %d: %d, %v", at, len(more), err)
				}
				hashes.Add(more)
			}
			if err = hashes.Check(int64(off), dec); err != nil {
				t.Fatalf("part %d at %d: %v", part, off, err)
			}
			got = append(got, dec...)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("parts of %d: the file differs", part)
		}
	}

	// 4 KB just before 1 MB: the counter is offset/16 there too.
	dec, err := mtclient.CdnDecrypt(s.Key, s.IV, 1<<20-4096, cipherText[1<<20-4096:1<<20])
	if err != nil || !bytes.Equal(dec, plain[1<<20-4096:1<<20]) {
		t.Fatalf("4 KB before 1 MB: %v", err)
	}
}

// TestCdnHashesRefuseCipher: hashes of the cipher, which a home must not
// send, fail the check, as they fail the downloads of the clients; so do a
// wrong length and a piece without a hash.
func TestCdnHashesRefuseCipher(t *testing.T) {
	const size = 1<<20 + 5
	plain, cipherText, _, blob := homeFile(t, size)
	h := cdn.NewHasher()
	_, _ = h.Write(cipherText)
	ofCipher, err := cdn.Answer(h.Sum(), size, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = mtclient.NewCdnHashes(ofCipher).Check(0, plain[:1<<20]); err == nil {
		t.Fatal("hashes of the cipher passed")
	}
	good, err := cdn.Answer(blob, size, 0)
	if err != nil {
		t.Fatal(err)
	}
	hashes := mtclient.NewCdnHashes(good)
	if err = hashes.Check(1<<20, plain[1<<20:]); err != nil {
		t.Fatalf("the last short piece: %v", err)
	}
	if err = hashes.Check(1<<20, plain[1<<20:size-1]); err == nil {
		t.Fatal("a piece shorter than its hash passed")
	}
	if err = mtclient.NewCdnHashes(good[:2]).Check(0, plain[:1<<20]); err == nil {
		t.Fatal("pieces without hashes passed")
	}
	if at, missing := mtclient.NewCdnHashes(good[:2]).Missing(0, 1<<20); !missing || at != 2*mtclient.CdnHashChunk {
		t.Fatalf("Missing = %d, %v", at, missing)
	}
}

func TestParseCdnRedirect(t *testing.T) {
	for name, f := range map[string]*mtproto.Upload_File{
		"a short key": {DcId: 201, FileToken: []byte{1}, EncryptionKey: make([]byte, 16), EncryptionIv: make([]byte, 16)},
		"a long IV":   {DcId: 201, FileToken: []byte{1}, EncryptionKey: make([]byte, 32), EncryptionIv: make([]byte, 32)},
		"no token":    {DcId: 201, EncryptionKey: make([]byte, 32), EncryptionIv: make([]byte, 16)},
	} {
		if _, err := mtclient.ParseCdnRedirect(mtproto.MakeTLUploadFileCdnRedirect(f).To_Upload_File()); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := mtclient.ParseCdnRedirect(mtproto.MakeTLUploadFile(&mtproto.Upload_File{Bytes: []byte{1}}).To_Upload_File()); err == nil {
		t.Error("upload.file taken for a redirect")
	}
}

func TestCdnPublicKey(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkcs1 := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&priv.PublicKey)}))
	pkix, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &mtproto.CdnConfig{PublicKeys: []*mtproto.CdnPublicKey{
		{DcId: 201, PublicKey: pkcs1},
		{DcId: 202, PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkix}))},
	}}
	pub, err := mtclient.CdnPublicKey(cfg, 201)
	if err != nil || !pub.Equal(&priv.PublicKey) {
		t.Fatalf("DC 201: %v", err)
	}
	if _, err = mtclient.CdnPublicKey(cfg, 202); err == nil {
		t.Error("a PKIX key, which Android cannot read, was taken")
	}
	if _, err = mtclient.CdnPublicKey(cfg, 203); err == nil {
		t.Error("a key of a DC not in the config")
	}
}
