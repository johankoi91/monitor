// pgprovision generates private deployment material; it never prints secrets.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func random() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		log.Fatal(e)
	}
	return hex.EncodeToString(b[:])
}
func main() {
	out := flag.String("out", "", "private output directory")
	flag.Parse()
	if *out == "" {
		log.Fatal("out required")
	}
	if e := os.MkdirAll(*out, 0700); e != nil {
		log.Fatal(e)
	}
	owner, runtime, key, admin := random(), random(), random(), random()
	dsn := func(user, secret, db string) string {
		return fmt.Sprintf("postgres://%s:%s@127.0.0.1:5432/%s?sslmode=disable", user, secret, db)
	}
	files := map[string]string{
		"postgres.env":        fmt.Sprintf("POSTGRES_USER=avops_owner\nPOSTGRES_PASSWORD=%s\nPOSTGRES_DB=avops_monitor\nPOSTGRES_INITDB_ARGS=--auth-host=scram-sha-256\n", owner),
		"owner.env":           fmt.Sprintf("AVOPS_DATABASE_URL=%s\nAVOPS_STORAGE_KEY=%s\n", dsn("avops_owner", owner, "avops_monitor"), key),
		"database.env":        fmt.Sprintf("AVOPS_DATABASE_URL=%s\nAVOPS_STORAGE_KEY=%s\nAVOPS_BOOTSTRAP_USERNAME=admin\nAVOPS_BOOTSTRAP_PASSWORD=%s\n", dsn("avops_runtime", runtime, "avops_monitor"), key, admin),
		"test.env":            fmt.Sprintf("AVOPS_TEST_DATABASE_URL=%s\nAVOPS_STORAGE_KEY=%s\n", dsn("avops_owner", owner, "avops_test"), key),
		"init.sql":            fmt.Sprintf("CREATE ROLE avops_runtime LOGIN PASSWORD '%s';\nCREATE SCHEMA IF NOT EXISTS avops AUTHORIZATION avops_owner;\nGRANT CONNECT ON DATABASE avops_monitor TO avops_runtime;\nGRANT USAGE ON SCHEMA avops TO avops_runtime;\nALTER DEFAULT PRIVILEGES FOR ROLE avops_owner IN SCHEMA avops GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO avops_runtime;\nALTER DEFAULT PRIVILEGES FOR ROLE avops_owner IN SCHEMA avops GRANT USAGE,SELECT ON SEQUENCES TO avops_runtime;\n", runtime),
		"test.sql":            "CREATE DATABASE avops_test OWNER avops_owner;\n",
		"account-handoff.txt": fmt.Sprintf("控制台账号：admin\n初始密码：%s\n首次登录后请修改密码。此文件禁止提交 Git。\n", admin),
	}
	for n := range files {
		if _, e := os.Stat(filepath.Join(*out, n)); !os.IsNotExist(e) {
			log.Fatal("existing deployment material retained")
		}
	}
	for n, b := range files {
		f, e := os.OpenFile(filepath.Join(*out, n), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			log.Fatal(e)
		}
		if _, e = f.WriteString(b); e == nil {
			e = f.Sync()
		}
		f.Close()
		if e != nil {
			log.Fatal(e)
		}
	}
	fmt.Println("Private PostgreSQL and account material created; no credentials printed")
}
