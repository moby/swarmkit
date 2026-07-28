// Command proto-name-fix applies field and type renames to protoc-gen-go
// generated *.pb.go files.  The rename rules are loaded from rename_map.json,
// which is produced by protoc-gen-goswarm in the same protoc invocation.
//
// Usage:
//
//	proto-name-fix -rename-map=<path>/rename_map.json <file1.pb.go> [...]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"os"
	"strings"
)

func main() {
	renameMapPath := flag.String("rename-map", "rename_map.json", "path to rename_map.json produced by protoc-gen-goswarm")
	flag.Parse()
	files := flag.Args()
	if len(files) == 0 {
		log.Fatal("proto-name-fix: no .pb.go files specified")
	}

	renames, err := loadRenameMap(*renameMapPath)
	if err != nil {
		log.Fatalf("proto-name-fix: loading rename map: %v", err)
	}
	if len(renames) == 0 {
		return
	}

	for _, path := range files {
		if err := fixFile(path, renames); err != nil {
			log.Fatalf("proto-name-fix: %s: %v", path, err)
		}
	}
}

type renameEntry struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func loadRenameMap(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	// Strip the leading blank line that protogen P() adds before content
	content := strings.TrimSpace(string(data))
	var entries []renameEntry
	if err := json.Unmarshal([]byte(content), &entries); err != nil {
		return nil, fmt.Errorf("parsing %s: %v", path, err)
	}
	renames := make(map[string]string, len(entries))
	for _, e := range entries {
		renames[e.From] = e.To
	}
	return renames, nil
}

// fixFile applies the rename map to a single Go file using AST manipulation.
func fixFile(path string, renames map[string]string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse: %v", err)
	}

	changed := false
	ast.Inspect(f, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if newName, ok := renames[ident.Name]; ok {
			ident.Name = newName
			changed = true
		}
		return true
	})

	if !changed {
		return nil
	}

	var buf strings.Builder
	if err := format.Node(&buf, fset, f); err != nil {
		return fmt.Errorf("format: %v", err)
	}

	return os.WriteFile(path, []byte(buf.String()), 0o644)
}
