package main

import (
	"embed"

	"github.com/AhmadEbeid/passwordless-auth-service/cmd"
)

//go:embed all:migrations
var migrationsFS embed.FS

func main() {
	cmd.Execute(migrationsFS)
}
