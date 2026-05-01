package rootcerts

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPoolFromDirsLoadsAndroidPEMCert(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: der,
	})
	if err := os.WriteFile(filepath.Join(dir, "test.0"), certPEM, 0o644); err != nil {
		t.Fatal(err)
	}

	pool := PoolFromDirs(x509.NewCertPool(), []string{dir})
	if len(pool.Subjects()) == 0 {
		t.Fatal("expected Android-style PEM cert file to be loaded")
	}
}
