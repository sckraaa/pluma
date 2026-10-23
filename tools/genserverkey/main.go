// Command genserverkey generates the MTProto server RSA key and prints what
// the server config and the client patches need in order to trust it.
//
// Usage (from the repo root):
//
//	go run ./tools/genserverkey -out secrets/server_pkcs1.key   # new key
//	go run ./tools/genserverkey -show secrets/server_pkcs1.key  # print an existing key
//
// -out writes a 2048-bit PKCS#1 PEM private key with mode 0600 and never
// overwrites an existing file: replacing the key breaks every client built
// with the old public key. -show accepts a private key or a public key in
// PKCS#1 or PKIX PEM form, e.g. to check the key embedded in a client patch.
//
// See docs/ops/security-setup.md.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strconv"

	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "genserverkey:", err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("genserverkey", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", "", "write a new private key to this path (mode 0600; an existing file is never overwritten)")
	show := flags.String("show", "", "print the fingerprint and public forms of an existing private or public key PEM file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || (*out == "") == (*show == "") {
		flags.Usage()
		return errors.New("exactly one of -out or -show is required")
	}

	if *show != "" {
		data, err := os.ReadFile(*show)
		if err != nil {
			return err
		}
		pub, err := serverkey.ParsePublicKeyPEM(data)
		if err != nil {
			return fmt.Errorf("%s: %w", *show, err)
		}
		fmt.Fprintf(stdout, "Key: %s\n\n", *show)
		return printPublic(stdout, pub)
	}

	key, err := generate(*out, rand.Reader)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Wrote a new %d-bit server private key to %s (mode 0600).\n", serverkey.Bits, *out)
	fmt.Fprintln(stdout, "Keep it secret: never commit it, never put it into an image; back it up offline.")
	fmt.Fprintln(stdout)
	return printPublic(stdout, &key.PublicKey)
}

// generate creates a new key at path, refusing to replace an existing file.
func generate(path string, random io.Reader) (*rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(random, serverkey.Bits)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	if dir := filepath.Dir(path); dir != "" {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("refusing to overwrite %s: a new key breaks every client built with the old public key; move the old file away first if you really mean to rotate", path)
		}
		return nil, err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write %s: %w", path, err)
	}

	// Read the file back the way gnetway will, so a key this tool reports is
	// known to load.
	if _, err = serverkey.Load(path, strconv.FormatUint(serverkey.Fingerprint(&key.PublicKey), 10)); err != nil {
		return nil, err
	}

	return key, nil
}

func printPublic(w io.Writer, pub *rsa.PublicKey) error {
	fp := serverkey.Fingerprint(pub)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(pub)})

	_, err := fmt.Fprintf(w, `Fingerprint:
  KeyFingerprint (gnetway.yaml / .env SERVER_RSA_KEY_FINGERPRINT): %d
  as signed TL long:                                               %d
  as hex (Android tgnet serverPublicKeysFingerprints):             0x%016x

Public key, PKCS#1 PEM (iOS MtProtoKit, Android tgnet, Desktop kPublicRSAKeys):
%s
Public key for Web K (rsaKeysManager.ts publisKeysHex):
  modulus:  %s
  exponent: %s
`,
		fp, int64(fp), fp,
		pubPEM,
		hex.EncodeToString(pub.N.Bytes()),
		hex.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	)

	return err
}
