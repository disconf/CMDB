package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"cmdb/gateway-bff/internal/license"
)

func main() {
	privatePath := flag.String("private", "", "private key output path")
	publicPath := flag.String("public", "", "public key output path")
	flag.Parse()
	if *privatePath == "" || *publicPath == "" {
		fmt.Fprintln(os.Stderr, "both -private and -public are required")
		os.Exit(2)
	}
	publicKey, privateKey, err := license.GenerateKeyPair()
	if err != nil {
		panic(err)
	}
	privatePEM, err := license.PrivateKeyPEM(privateKey)
	if err != nil {
		panic(err)
	}
	publicPEM, err := license.PublicKeyPEM(publicKey)
	if err != nil {
		panic(err)
	}
	for path, data := range map[string][]byte{*privatePath: privatePEM, *publicPath: publicPEM} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			panic(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			panic(err)
		}
	}
	fmt.Println("license signing key pair generated")
}
