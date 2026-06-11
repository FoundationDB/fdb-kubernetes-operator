/*
 * main.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// check-license verifies that every Go source file starts with the canonical
// license header defined in hack/boilerplate.go.txt.  Run with --fix to
// automatically add or replace non-conforming headers.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	boilerplatePath := flag.String(
		"boilerplate",
		"hack/boilerplate.go.txt",
		"path to the boilerplate license header file",
	)
	fix := flag.Bool(
		"fix",
		false,
		"add or replace non-conforming headers instead of reporting violations",
	)
	flag.Parse()

	boilerplateRaw, err := os.ReadFile(*boilerplatePath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cannot read boilerplate: %v\n", err)
		os.Exit(2)
	}

	// Trim any trailing newline so string operations are predictable.
	bpRaw := strings.TrimRight(string(boilerplateRaw), "\n")
	bpInner := extractInner(bpRaw)

	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}

	var violations []string
	for _, root := range roots {
		walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skipDir(path, d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !isGoSource(path, d.Name()) {
				return nil
			}

			filename := d.Name()
			if *fix {
				if cErr := checkFile(path, filename, bpInner); cErr != nil {
					if fErr := fixFile(path, filename, bpRaw); fErr != nil {
						_, _ = fmt.Fprintf(os.Stderr, "fix %s: %v\n", path, fErr)
					} else {
						fmt.Println("fixed:", path)
					}
				}
				return nil
			}

			if cErr := checkFile(path, filename, bpInner); cErr != nil {
				violations = append(violations, fmt.Sprintf("%s: %v", path, cErr))
			}
			return nil
		})
		if walkErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "walk %s: %v\n", root, walkErr)
			os.Exit(2)
		}
	}

	if len(violations) > 0 {
		for _, v := range violations {
			_, _ = fmt.Fprintln(os.Stderr, v)
		}
		_, _ = fmt.Fprintf(
			os.Stderr,
			"\n%d file(s) with license violations. Run with --fix to repair.\n",
			len(violations),
		)
		os.Exit(1)
	}
}

// skipDir reports whether a directory should be skipped entirely.
// path is the full walk path; name is the directory's base name.
// The walk root itself is never skipped regardless of its name.
func skipDir(path, name string) bool {
	if path == name || path == "./"+name {
		// This is the root directory — never skip it.
		return false
	}
	return name == "vendor" || name == "bin" || strings.HasPrefix(name, ".")
}

// isGoSource reports whether a file should be checked.
// Generated files and the e2e chaos-mesh directory are excluded (matching the
// golangci-lint exclusions in .golangci.yml).
func isGoSource(path, name string) bool {
	if !strings.HasSuffix(name, ".go") {
		return false
	}
	if strings.HasPrefix(name, "zz_generated.") {
		return false
	}
	// Matches the golangci-lint exclusion for e2e/chaos-mesh.
	if strings.Contains(filepath.ToSlash(path), "e2e/chaos-mesh") {
		return false
	}

	// Ignore the auto generated applyconfiguration.
	if strings.Contains(filepath.ToSlash(path), "api/v1beta2/applyconfiguration") {
		return false
	}

	// Should be ignored as it already has an Apache header.
	if strings.Contains(filepath.ToSlash(path), "cmd/po-docgen") {
		return false
	}

	return true
}

// extractInner returns the text between /* and */ in a boilerplate block
// comment, with the surrounding delimiters and leading/trailing newlines removed.
func extractInner(raw string) string {
	inner := strings.TrimPrefix(raw, "/*")
	inner = strings.TrimSuffix(inner, "*/")
	return strings.Trim(inner, "\n")
}

// buildHeader inserts filename as the first line of the boilerplate block comment:
//
//	/*
//	filename.go
//
//	Copyright 2018-2026 FoundationDB project authors.
//	...
//	*/
func buildHeader(filename, bpRaw string) string {
	body := strings.TrimPrefix(bpRaw, "/*\n")
	return "/*\n * " + filename + "\n *\n" + body
}

// checkFile returns an error when path is missing a conforming license header.
func checkFile(path, filename, bpInner string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(data)
	if !strings.HasPrefix(s, "/*") {
		return fmt.Errorf("missing license header")
	}

	end := strings.Index(s, "*/")
	if end < 0 {
		return fmt.Errorf("unclosed block comment")
	}
	// Content between /* and */.
	header := s[2:end]
	if !strings.Contains(header, filename) {
		return fmt.Errorf("filename %q not found in header", filename)
	}
	if !strings.Contains(header, bpInner) {
		return fmt.Errorf("license text missing or outdated")
	}

	return nil
}

// fixFile writes a canonical header to path, replacing any existing leading
// block comment.
func fixFile(path, filename, bpRaw string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(data)

	// Strip existing leading block comment if present.
	rest := s
	if strings.HasPrefix(s, "/*") {
		_, after, ok := strings.Cut(s, "*/")
		if ok {
			rest = strings.TrimLeft(after, "\n")
		}
	}

	canonical := buildHeader(filename, bpRaw)
	return os.WriteFile(path, []byte(canonical+"\n\n"+rest), 0o644)
}
