package rootcerts

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
)

var androidRootCAPaths = []string{
	"/apex/com.android.conscrypt/cacerts",
	"/system/etc/security/cacerts",
	"/data/misc/keychain/certs-added",
	"/data/misc/user/0/cacerts-added",
}

func AndroidSystemPool() *x509.CertPool {
	pool, _ := x509.SystemCertPool()
	return PoolFromDirs(pool, androidRootCAPaths)
}

func PoolFromDirs(pool *x509.CertPool, dirs []string) *x509.CertPool {
	if pool == nil {
		pool = x509.NewCertPool()
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			AppendCertsFromPEMOrDER(pool, data)
		}
	}
	if len(pool.Subjects()) == 0 {
		return nil
	}
	return pool
}

func AppendCertsFromPEMOrDER(pool *x509.CertPool, data []byte) {
	if pool.AppendCertsFromPEM(data) {
		return
	}
	if cert, err := x509.ParseCertificate(data); err == nil {
		pool.AddCert(cert)
		return
	}
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
			pool.AddCert(cert)
		}
	}
}
