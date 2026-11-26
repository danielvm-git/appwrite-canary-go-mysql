// Package db opens the pooled MySQL connection to Oracle MySQL HeatWave.
//
// HeatWave always requires TLS and its endpoint certificate is issued by a
// per-endpoint CA whose subject is only `CN = MySQL_Endpoint_Server` — there is
// no SAN, so Go's default hostname verification can never succeed. We therefore
// keep the chain check (VerifyPeerCertificate against the endpoint CA) and skip
// only the hostname check. Do not "fix" this by disabling verification.
package db

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/config"
)

const tlsConfigName = "heatwave"

// Open returns a pool sized for a function runtime (few concurrent invocations).
func Open(c config.Config) (*sql.DB, error) {
	if c.DBPassword == "" {
		return nil, errors.New("db: MYSQL_PASSWORD is empty")
	}

	if err := registerTLS(c); err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?parseTime=true&loc=UTC&timeout=5s&readTimeout=15s&writeTimeout=15s&tls=%s&charset=utf8mb4",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, tlsConfigName,
	)

	pool, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	pool.SetMaxOpenConns(config.IntEnv("MYSQL_POOL_MAX", 4))
	pool.SetMaxIdleConns(config.IntEnv("MYSQL_POOL_IDLE", 2))
	pool.SetConnMaxLifetime(5 * time.Minute)

	if err := pool.Ping(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

func registerTLS(c config.Config) error {
	pem := c.DBTLSCA
	if pem == "" && c.DBTLSCAPath != "" {
		data, err := os.ReadFile(c.DBTLSCAPath)
		if err != nil {
			return fmt.Errorf("db: read MYSQL_CA_PATH: %w", err)
		}
		pem = string(data)
	}
	if pem == "" {
		return errors.New("db: set MYSQL_CA_PEM (or MYSQL_CA_PATH) with the HeatWave endpoint CA")
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(pem)) {
		return errors.New("db: MYSQL_CA_PEM is not a valid PEM certificate")
	}

	cfg := &tls.Config{
		RootCAs: roots,
		// No SAN on the endpoint certificate; chain verification happens below.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("db: server presented no certificate")
			}
			leaf, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return fmt.Errorf("db: parse server certificate: %w", err)
			}
			inter := x509.NewCertPool()
			for _, raw := range rawCerts[1:] {
				cert, err := x509.ParseCertificate(raw)
				if err != nil {
					return fmt.Errorf("db: parse intermediate: %w", err)
				}
				inter.AddCert(cert)
			}
			if _, err := leaf.Verify(x509.VerifyOptions{
				Roots:         roots,
				Intermediates: inter,
				KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			}); err != nil {
				return fmt.Errorf("db: server certificate is not signed by the endpoint CA: %w", err)
			}
			return nil
		},
	}

	// A function process registers once; tests re-enter, and a duplicate name is
	// harmless because the config is identical.
	_ = mysql.RegisterTLSConfig(tlsConfigName, cfg)
	return nil
}
