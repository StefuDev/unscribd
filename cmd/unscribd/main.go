package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime/pprof"

	"github.com/StefuDev/unscribd/internal/unscribd"
)

func main() {
	output := flag.String("o", "", "output file or directory")
	images := flag.String("images-dir", "", "directory for page images")
	format := flag.String("format", "pdf", "pdf, images, jsonp, or text")
	concurrency := flag.Int("concurrency", 6, "parallel page requests (1-32)")
	token := flag.String("token", "", "JWT token")
	noToken := flag.Bool("no-token", false, "do not fetch a token")
	cacheDir := flag.String("cache-dir", "", "persistent cache directory (optional)")
	cacheBytes := flag.Int64("cache-bytes", 256<<20, "maximum cache size in bytes")
	cpuProfile := flag.String("cpuprofile", "", "write a Go CPU profile for PGO")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: unscribd [flags] <Scribd URL or document ID>")
		os.Exit(2)
	}
	if *concurrency < 1 || *concurrency > 32 {
		fmt.Fprintln(os.Stderr, "--concurrency must be between 1 and 32")
		os.Exit(2)
	}
	if *format != "pdf" && *format != "images" && *format != "jsonp" && *format != "text" {
		fmt.Fprintln(os.Stderr, "--format must be pdf, images, jsonp, or text")
		os.Exit(2)
	}
	if *cpuProfile != "" {
		profileFile, err := os.Create(*cpuProfile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "[error] create CPU profile:", err)
			os.Exit(1)
		}
		if err := pprof.StartCPUProfile(profileFile); err != nil {
			_ = profileFile.Close()
			fmt.Fprintln(os.Stderr, "[error] start CPU profile:", err)
			os.Exit(1)
		}
		defer func() {
			pprof.StopCPUProfile()
			_ = profileFile.Close()
		}()
	}
	result, err := unscribd.Download(context.Background(), flag.Arg(0), unscribd.Options{
		Format:      *format,
		Output:      *output,
		ImagesDir:   *images,
		Token:       *token,
		NoToken:     *noToken,
		CacheDir:    *cacheDir,
		CacheBytes:  *cacheBytes,
		Concurrency: *concurrency,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "[error]", err)
		os.Exit(1)
	}
	fmt.Printf("[done] %s (%d pages): %s\n", result.Title, result.Pages, result.Output)
}
