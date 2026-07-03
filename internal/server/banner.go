package server

import (
	"fmt"

	"dockyard/config"
	"dockyard/internal/version"
)

func printBanner(cfg *config.Config) {
	const (
		cyan   = "\033[36m"
		gray   = "\033[90m"
		yellow = "\033[33m"
		white  = "\033[37m"
		reset  = "\033[0m"
	)

	row := func(key, val string) string {
		return fmt.Sprintf("%s  │%s  %s%-10s%s  %s%-52s%s %s│%s\n",
			gray, reset, yellow, key, reset, white, val, reset, gray, reset)
	}

	v2auth := "off"
	if cfg.V2AuthEnabled {
		v2auth = "on"
	}

	fmt.Print("\n" +
		cyan + "  ██████╗  ██████╗  ██████╗██╗  ██╗██╗   ██╗ █████╗ ██████╗ ██████╗ " + reset + "\n" +
		cyan + "  ██╔══██╗██╔═══██╗██╔════╝██║ ██╔╝╚██╗ ██╔╝██╔══██╗██╔══██╗██╔══██╗" + reset + "\n" +
		cyan + "  ██║  ██║██║   ██║██║     █████╔╝  ╚████╔╝ ███████║██████╔╝██║  ██║" + reset + "\n" +
		cyan + "  ██║  ██║██║   ██║██║     ██╔═██╗   ╚██╔╝  ██╔══██║██╔══██╗██║  ██║" + reset + "\n" +
		cyan + "  ██████╔╝╚██████╔╝╚██████╗██║  ██╗   ██║   ██║  ██║██║  ██║██████╔╝" + reset + "\n" +
		cyan + "  ╚═════╝  ╚═════╝  ╚═════╝╚═╝  ╚═╝   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝ " + reset + "\n" +
		gray + "  Self-hosted Docker Registry V2" + reset + "\n" +
		"\n" +
		gray + "  ┌───────────────────────────────────────────────────────────┐" + reset + "\n" +
		row("version", version.Version) +
		row("mode", cfg.RegistryMode) +
		row("storage", storageLabel(cfg)) +
		row("port", fmt.Sprintf(":%d", cfg.Port)) +
		row("v2 auth", v2auth) +
		gray + "  └───────────────────────────────────────────────────────────┘" + reset + "\n" +
		"\n")
}

func storageLabel(cfg *config.Config) string {
	if cfg.StorageBackend == config.StorageS3 {
		return fmt.Sprintf("s3  %s/%s", cfg.S3Endpoint, cfg.S3Bucket)
	}
	return fmt.Sprintf("local  %s", cfg.StoragePath)
}
