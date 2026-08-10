// SPDX-License-Identifier: MPL-2.0

package scipanalyzer

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
	scippb "github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

func TestImportIndexesBuildsDefinitionsCallsAndImplementations(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/main/java/example/App.java"), []byte("package example;\nclass App implements Runnable {\n  void run() { println(); }\n}\n"))

	classSymbol := "scip-java maven example app 1.0 example/App#"
	methodSymbol := "scip-java maven example app 1.0 example/App#run()."
	runnableSymbol := "scip-java jdk java.base 21 java/lang/Runnable#"
	printlnSymbol := "scip-java jdk java.base 21 java/io/PrintStream#println()."
	index := &scippb.Index{Documents: []*scippb.Document{{
		Language: "java", RelativePath: "src/main/java/example/App.java",
		Symbols: []*scippb.SymbolInformation{
			{Symbol: classSymbol, DisplayName: "App", Kind: scippb.SymbolInformation_Class,
				Relationships: []*scippb.Relationship{{Symbol: runnableSymbol, IsImplementation: true}}},
			{Symbol: methodSymbol, DisplayName: "run", Kind: scippb.SymbolInformation_Method,
				EnclosingSymbol: classSymbol, SignatureDocumentation: &scippb.Signature{Language: "java", Text: "void run()"}},
		},
		Occurrences: []*scippb.Occurrence{
			{Range: []int32{1, 6, 9}, EnclosingRange: []int32{1, 0, 3, 1}, Symbol: classSymbol, SymbolRoles: int32(scippb.SymbolRole_Definition), SyntaxKind: scippb.SyntaxKind_IdentifierType},
			{Range: []int32{2, 7, 10}, EnclosingRange: []int32{2, 2, 2, 27}, Symbol: methodSymbol, SymbolRoles: int32(scippb.SymbolRole_Definition), SyntaxKind: scippb.SyntaxKind_IdentifierFunctionDefinition},
			{Range: []int32{2, 15, 22}, Symbol: printlnSymbol, SyntaxKind: scippb.SyntaxKind_IdentifierFunction},
		},
	}}}
	encoded, err := proto.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(t.TempDir(), "index.scip")
	writeTestFile(t, indexPath, encoded)

	entities, relations, statistics, err := importIndexes(importRequest{
		RepositoryPath: root, RepositoryName: "example", Branch: "main", Revision: "abc123",
		IndexFiles: []string{indexPath}, Language: "jvm",
	})
	if err != nil {
		t.Fatal(err)
	}
	classKey := codegraph.StableKey("java", codegraph.EntityType, classSymbol)
	methodKey := codegraph.StableKey("java", codegraph.EntityMethod, methodSymbol)
	runnableKey := codegraph.StableKey("java", codegraph.EntityType, runnableSymbol)
	printlnKey := codegraph.StableKey("java", codegraph.EntityFunction, printlnSymbol)
	assertTestEntity(t, entities, classKey)
	method := assertTestEntity(t, entities, methodKey)
	if method.Signature != "void run()" || method.Location.Start.Line != 3 {
		t.Fatalf("method = %#v", method)
	}
	if assertTestEntity(t, entities, runnableKey).Metadata["external"] != "true" {
		t.Fatal("implementation target was not marked external")
	}
	assertTestEntity(t, entities, printlnKey)
	assertTestRelation(t, relations, codegraph.RelationImplements, classKey, runnableKey)
	assertTestRelation(t, relations, codegraph.RelationDefines, classKey, methodKey)
	assertTestRelation(t, relations, codegraph.RelationCalls, methodKey, printlnKey)
	if statistics["files"] != 1 || statistics["indexes"] != 1 {
		t.Fatalf("statistics = %#v", statistics)
	}
}

func TestImportIndexesRejectsEscapingDocumentPath(t *testing.T) {
	index := &scippb.Index{Documents: []*scippb.Document{{Language: "python", RelativePath: "../secret.py"}}}
	encoded, err := proto.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(t.TempDir(), "index.scip")
	writeTestFile(t, indexPath, encoded)
	_, _, _, err = importIndexes(importRequest{RepositoryPath: t.TempDir(), IndexFiles: []string{indexPath}, Language: "python"})
	if err == nil {
		t.Fatal("expected unsafe path rejection")
	}
}

func TestImportIndexesPrefixesNestedSCIPProjectRoot(t *testing.T) {
	root := t.TempDir()
	stagedRoot := t.TempDir()
	writeTestFile(t, filepath.Join(root, "web/src/app.ts"), []byte("export const app = true\n"))
	projectRoot := filepath.Join(stagedRoot, "web")
	index := &scippb.Index{
		Metadata:  &scippb.Metadata{ProjectRoot: (&url.URL{Scheme: "file", Path: projectRoot}).String()},
		Documents: []*scippb.Document{{Language: "typescript", RelativePath: "src/app.ts"}},
	}
	encoded, err := proto.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(t.TempDir(), "index.scip")
	writeTestFile(t, indexPath, encoded)

	entities, _, _, err := importIndexes(importRequest{
		RepositoryPath: root, StagedRepositoryPath: stagedRoot,
		RepositoryName: "example", Branch: "main", Revision: "abc123",
		IndexFiles: []string{indexPath}, Language: "typescript",
	})
	if err != nil {
		t.Fatal(err)
	}
	file := assertTestEntity(t, entities, codegraph.StableKey("typescript", codegraph.EntityFile, "web/src/app.ts"))
	if file.QualifiedName != "web/src/app.ts" {
		t.Fatalf("file = %#v", file)
	}
}

func assertTestEntity(t *testing.T, entities []codegraph.Entity, key string) codegraph.Entity {
	t.Helper()
	for _, entity := range entities {
		if entity.Key == key {
			return entity
		}
	}
	t.Fatalf("entity %q not found", key)
	return codegraph.Entity{}
}

func assertTestRelation(t *testing.T, relations []codegraph.Relation, kind codegraph.RelationKind, source, target string) {
	t.Helper()
	for _, relation := range relations {
		if relation.Kind == kind && relation.SourceKey == source && relation.TargetKey == target {
			return
		}
	}
	t.Fatalf("relation %s %s -> %s not found", kind, source, target)
}

func writeTestFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
