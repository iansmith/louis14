package main

import (
	"fmt"
	"os"

	"louis14/pkg/visualtest"
)

// Simple tool to generate reference images for visual regression tests
func main() {
	if len(os.Args) < 2 {
		fmt.Println("Reference Image Generator for Louis14")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  go run cmd/update-references/main.go <phase>")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  go run cmd/update-references/main.go phase1")
		fmt.Println("  go run cmd/update-references/main.go all")
		fmt.Println()
		fmt.Println("Or use the test-based approach:")
		fmt.Println("  UPDATE_REFS=1 go test -v ./cmd/l14open -run TestVisual")
		os.Exit(1)
	}

	phase := os.Args[1]

	switch phase {
	case "phase1", "all":
		if err := generatePhase1References(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ Phase 1 reference images generated successfully")

	default:
		fmt.Fprintf(os.Stderr, "Unknown phase: %s\n", phase)
		os.Exit(1)
	}
}

func generatePhase1References() error {
	const (
		width  = 800
		height = 600
	)

	references := []struct {
		htmlPath      string
		referencePath string
	}{
		{
			"testdata/phase1/simple.html",
			"testdata/phase1/reference/simple.png",
		},
	}

	for _, ref := range references {
		fmt.Printf("Generating: %s\n", ref.referencePath)
		if err := visualtest.UpdateReferenceImage(ref.htmlPath, ref.referencePath, width, height); err != nil {
			return fmt.Errorf("failed to generate %s: %w", ref.referencePath, err)
		}
	}

	return nil
}
