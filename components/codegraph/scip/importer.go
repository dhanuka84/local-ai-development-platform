// SPDX-License-Identifier: MPL-2.0

// Package scipanalyzer imports compiler-produced SCIP indexes into the
// platform's language-neutral code graph.
package scipanalyzer

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
	scippb "github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

const (
	defaultMaxFiles     = 5000
	defaultMaxEntities  = 200000
	defaultMaxRelations = 1000000
)

type importRequest struct {
	RepositoryPath       string
	StagedRepositoryPath string
	RepositoryName       string
	Branch               string
	Revision             string
	IndexFiles           []string
	Language             string
	MaxFiles             int
	MaxEntities          int
	MaxRelations         int
}

type indexedDocument struct {
	document *scippb.Document
	language string
	fileKey  string
	path     string
	defs     []definition
}

type definition struct {
	symbol string
	key    string
	kind   codegraph.EntityKind
	range_ codegraph.Location
}

type importer struct {
	root           string
	stagedRoot     string
	repositoryName string
	branch         string
	revision       string
	language       string
	maxFiles       int
	maxEntities    int
	maxRelations   int
	entities       map[string]codegraph.Entity
	relations      map[string]codegraph.Relation
	symbolKeys     map[string]string
	infos          map[string]*scippb.SymbolInformation
	documents      []*indexedDocument
	files          map[string]struct{}
}

func importIndexes(request importRequest) ([]codegraph.Entity, []codegraph.Relation, map[string]int64, error) {
	b := &importer{
		root: request.RepositoryPath, stagedRoot: request.StagedRepositoryPath,
		repositoryName: request.RepositoryName, branch: request.Branch,
		revision: request.Revision, language: normalizeLanguage(request.Language),
		maxFiles:     positiveOr(request.MaxFiles, defaultMaxFiles),
		maxEntities:  positiveOr(request.MaxEntities, defaultMaxEntities),
		maxRelations: positiveOr(request.MaxRelations, defaultMaxRelations),
		entities:     map[string]codegraph.Entity{}, relations: map[string]codegraph.Relation{},
		symbolKeys: map[string]string{}, infos: map[string]*scippb.SymbolInformation{}, files: map[string]struct{}{},
	}
	if err := b.load(request.IndexFiles); err != nil {
		return nil, nil, nil, err
	}
	if err := b.build(); err != nil {
		return nil, nil, nil, err
	}
	entities := make([]codegraph.Entity, 0, len(b.entities))
	for _, entity := range b.entities {
		entities = append(entities, entity)
	}
	relations := make([]codegraph.Relation, 0, len(b.relations))
	for _, relation := range b.relations {
		relations = append(relations, relation)
	}
	return entities, relations, map[string]int64{
		"files": int64(len(b.files)), "indexes": int64(len(request.IndexFiles)),
	}, nil
}

func (b *importer) load(paths []string) error {
	seenDocuments := map[string]struct{}{}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read SCIP index %s: %w", path, err)
		}
		index := &scippb.Index{}
		if err := proto.Unmarshal(contents, index); err != nil {
			return fmt.Errorf("decode SCIP index %s: %w", path, err)
		}
		prefix, err := projectRelativePrefix(b.stagedRoot, index.GetMetadata().GetProjectRoot())
		if err != nil {
			return fmt.Errorf("SCIP project root in %s: %w", path, err)
		}
		for _, document := range index.GetDocuments() {
			documentPath := document.GetRelativePath()
			if prefix != "" {
				documentPath = filepath.ToSlash(filepath.Join(prefix, filepath.FromSlash(documentPath)))
			}
			relative, err := safeRelativePath(documentPath)
			if err != nil {
				return fmt.Errorf("SCIP document: %w", err)
			}
			language := normalizeLanguage(document.GetLanguage())
			if language == "" {
				language = b.language
			}
			identity := language + "\x00" + relative
			if _, seen := seenDocuments[identity]; seen {
				continue
			}
			seenDocuments[identity] = struct{}{}
			b.documents = append(b.documents, &indexedDocument{document: document, language: language, path: relative})
			for _, info := range document.GetSymbols() {
				if info.GetSymbol() != "" {
					b.infos[info.GetSymbol()] = info
				}
			}
		}
	}
	if len(b.documents) == 0 {
		return fmt.Errorf("SCIP index contains no documents")
	}
	return nil
}

func projectRelativePrefix(stagedRoot, encodedProjectRoot string) (string, error) {
	stagedRoot = strings.TrimSpace(stagedRoot)
	encodedProjectRoot = strings.TrimSpace(encodedProjectRoot)
	if stagedRoot == "" || encodedProjectRoot == "" {
		return "", nil
	}
	parsed, err := url.Parse(encodedProjectRoot)
	if err != nil {
		return "", fmt.Errorf("decode %q: %w", encodedProjectRoot, err)
	}
	var projectRoot string
	switch parsed.Scheme {
	case "":
		projectRoot = encodedProjectRoot
	case "file":
		if parsed.Host != "" && parsed.Host != "localhost" {
			return "", fmt.Errorf("file URI host %q is not local", parsed.Host)
		}
		projectRoot, err = url.PathUnescape(parsed.Path)
		if err != nil {
			return "", fmt.Errorf("decode file URI path: %w", err)
		}
	default:
		return "", fmt.Errorf("unsupported URI scheme %q", parsed.Scheme)
	}
	relative, err := filepath.Rel(filepath.Clean(stagedRoot), filepath.Clean(filepath.FromSlash(projectRoot)))
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("project root %q is outside the staged repository", projectRoot)
	}
	if relative == "." {
		return "", nil
	}
	return filepath.ToSlash(relative), nil
}

func (b *importer) build() error {
	repositoryKey := codegraph.StableKey(b.language, codegraph.EntityRepository, ".")
	if err := b.addEntity(codegraph.Entity{
		Key: repositoryKey, Language: b.language, Kind: codegraph.EntityRepository,
		Name: b.repositoryName, QualifiedName: ".", Metadata: map[string]string{
			"path": b.root, "repository": b.repositoryName, "branch": b.branch, "revision": b.revision,
		},
	}); err != nil {
		return err
	}
	packageKeys := map[string]string{}
	for _, document := range b.documents {
		if _, exists := b.files[document.path]; !exists {
			b.files[document.path] = struct{}{}
			if len(b.files) > b.maxFiles {
				return fmt.Errorf("file limit exceeded: %d", b.maxFiles)
			}
		}
		contents, err := os.ReadFile(filepath.Join(b.root, filepath.FromSlash(document.path)))
		if err != nil && document.document.GetText() == "" {
			return fmt.Errorf("read indexed source %s: %w", document.path, err)
		}
		if len(contents) == 0 && document.document.GetText() != "" {
			contents = []byte(document.document.GetText())
		}
		document.fileKey = codegraph.StableKey(document.language, codegraph.EntityFile, document.path)
		if err := b.addEntity(codegraph.Entity{
			Key: document.fileKey, Language: document.language, Kind: codegraph.EntityFile,
			Name: filepath.Base(document.path), QualifiedName: document.path, ContentHash: codegraph.HashContent(contents),
		}); err != nil {
			return err
		}
		packageName := filepath.ToSlash(filepath.Dir(document.path))
		packageIdentity := document.language + "\x00" + packageName
		packageKey, exists := packageKeys[packageIdentity]
		if !exists {
			packageKey = codegraph.StableKey(document.language, codegraph.EntityPackage, packageName)
			packageKeys[packageIdentity] = packageKey
			if err := b.addEntity(codegraph.Entity{
				Key: packageKey, Language: document.language, Kind: codegraph.EntityPackage,
				Name: filepath.Base(packageName), QualifiedName: packageName,
			}); err != nil {
				return err
			}
			if err := b.addRelation(codegraph.Relation{SourceKey: repositoryKey, TargetKey: packageKey, Kind: codegraph.RelationContains, Confidence: 1}); err != nil {
				return err
			}
		}
		if err := b.addRelation(codegraph.Relation{SourceKey: packageKey, TargetKey: document.fileKey, Kind: codegraph.RelationContains, Confidence: 1}); err != nil {
			return err
		}
	}

	for _, document := range b.documents {
		if err := b.addDefinitions(document); err != nil {
			return err
		}
	}
	for _, document := range b.documents {
		if err := b.addReferences(document); err != nil {
			return err
		}
	}
	return nil
}

func (b *importer) addDefinitions(document *indexedDocument) error {
	definitionOccurrences := map[string]*scippb.Occurrence{}
	for _, occurrence := range document.document.GetOccurrences() {
		if hasRole(occurrence, scippb.SymbolRole_Definition) {
			definitionOccurrences[occurrence.GetSymbol()] = occurrence
		}
	}
	for _, info := range document.document.GetSymbols() {
		kind, ok := entityKind(info.GetKind())
		if !ok || info.GetSymbol() == "" {
			continue
		}
		occurrence := definitionOccurrences[info.GetSymbol()]
		if occurrence == nil {
			continue
		}
		location := occurrenceLocation(document.path, occurrence, false)
		if hasRole(occurrence, scippb.SymbolRole_Test) || isTestName(info.GetDisplayName(), document.path) {
			if kind == codegraph.EntityFunction || kind == codegraph.EntityMethod {
				kind = codegraph.EntityTest
			}
		}
		key := codegraph.StableKey(document.language, kind, info.GetSymbol())
		metadata := map[string]string{"scip_symbol": info.GetSymbol()}
		if documentation := strings.TrimSpace(strings.Join(info.GetDocumentation(), "\n\n")); documentation != "" {
			metadata["documentation"] = documentation
		}
		entity := codegraph.Entity{
			Key: key, Language: document.language, Kind: kind, Name: displayName(info),
			QualifiedName: info.GetSymbol(), Location: location, Metadata: metadata,
			ContentHash: codegraph.HashContent([]byte(info.GetSymbol() + "\n" + signatureText(info) + "\n" + metadata["documentation"])),
			Signature:   signatureText(info),
		}
		if err := b.addEntity(entity); err != nil {
			return err
		}
		b.symbolKeys[info.GetSymbol()] = key
		document.defs = append(document.defs, definition{symbol: info.GetSymbol(), key: key, kind: kind, range_: occurrenceLocation(document.path, occurrence, true)})
		if err := b.addRelation(codegraph.Relation{SourceKey: document.fileKey, TargetKey: key, Kind: codegraph.RelationDefines, Location: location, Confidence: 1}); err != nil {
			return err
		}
	}
	for _, info := range document.document.GetSymbols() {
		sourceKey := b.symbolKeys[info.GetSymbol()]
		if sourceKey == "" {
			continue
		}
		if enclosingKey := b.symbolKeys[info.GetEnclosingSymbol()]; enclosingKey != "" {
			if err := b.addRelation(codegraph.Relation{SourceKey: enclosingKey, TargetKey: sourceKey, Kind: codegraph.RelationDefines, Confidence: 1}); err != nil {
				return err
			}
		}
		for _, relationship := range info.GetRelationships() {
			if !relationship.GetIsImplementation() {
				continue
			}
			targetKey, err := b.ensureReferencedEntity(document.language, relationship.GetSymbol(), scippb.SyntaxKind_IdentifierType)
			if err != nil {
				return err
			}
			if targetKey != "" {
				if err := b.addRelation(codegraph.Relation{SourceKey: sourceKey, TargetKey: targetKey, Kind: codegraph.RelationImplements, Evidence: "SCIP relationship", Confidence: 1}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (b *importer) addReferences(document *indexedDocument) error {
	for _, occurrence := range document.document.GetOccurrences() {
		if occurrence.GetSymbol() == "" || hasRole(occurrence, scippb.SymbolRole_Definition) {
			continue
		}
		targetKey, err := b.ensureReferencedEntity(document.language, occurrence.GetSymbol(), occurrence.GetSyntaxKind())
		if err != nil {
			return err
		}
		if targetKey == "" {
			continue
		}
		location := occurrenceLocation(document.path, occurrence, false)
		source := enclosingDefinition(document.defs, location)
		sourceKey := document.fileKey
		sourceKind := codegraph.EntityFile
		if source != nil {
			sourceKey, sourceKind = source.key, source.kind
		}
		kind := codegraph.RelationReferences
		switch {
		case hasRole(occurrence, scippb.SymbolRole_Import):
			sourceKey, kind = document.fileKey, codegraph.RelationImports
		case occurrence.GetSyntaxKind() == scippb.SyntaxKind_IdentifierFunction:
			kind = codegraph.RelationCalls
			if sourceKind == codegraph.EntityTest {
				kind = codegraph.RelationTests
			}
		}
		if err := b.addRelation(codegraph.Relation{
			SourceKey: sourceKey, TargetKey: targetKey, Kind: kind, Evidence: occurrence.GetSymbol(),
			Location: location, Confidence: 1,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (b *importer) ensureReferencedEntity(language, symbol string, syntax scippb.SyntaxKind) (string, error) {
	if symbol == "" || strings.HasPrefix(symbol, "local ") {
		return "", nil
	}
	if key := b.symbolKeys[symbol]; key != "" {
		return key, nil
	}
	info := b.infos[symbol]
	kind, ok := entityKind(scippb.SymbolInformation_UnspecifiedKind)
	if info != nil {
		kind, ok = entityKind(info.GetKind())
	}
	if !ok {
		kind, ok = inferredKind(syntax)
	}
	if !ok {
		return "", nil
	}
	key := codegraph.StableKey(language, kind, symbol)
	entity := codegraph.Entity{
		Key: key, Language: language, Kind: kind, Name: symbolTail(symbol), QualifiedName: symbol,
		Metadata: map[string]string{"external": "true", "scip_symbol": symbol},
	}
	if info != nil {
		entity.Name = displayName(info)
		entity.Signature = signatureText(info)
		if documentation := strings.TrimSpace(strings.Join(info.GetDocumentation(), "\n\n")); documentation != "" {
			entity.Metadata["documentation"] = documentation
		}
	}
	if err := b.addEntity(entity); err != nil {
		return "", err
	}
	b.symbolKeys[symbol] = key
	return key, nil
}

func (b *importer) addEntity(entity codegraph.Entity) error {
	if existing, ok := b.entities[entity.Key]; ok {
		if existing.Location.FilePath == "" && entity.Location.FilePath != "" {
			b.entities[entity.Key] = entity
		}
		return nil
	}
	if len(b.entities) >= b.maxEntities {
		return fmt.Errorf("entity limit exceeded: %d", b.maxEntities)
	}
	b.entities[entity.Key] = entity
	return nil
}

func (b *importer) addRelation(relation codegraph.Relation) error {
	if relation.SourceKey == "" || relation.TargetKey == "" {
		return nil
	}
	identity := relation.SourceKey + "\x00" + string(relation.Kind) + "\x00" + relation.TargetKey + "\x00" +
		relation.Location.FilePath + "\x00" + strconv.Itoa(relation.Location.Start.Line) + ":" + strconv.Itoa(relation.Location.Start.Column)
	if _, ok := b.relations[identity]; ok {
		return nil
	}
	if len(b.relations) >= b.maxRelations {
		return fmt.Errorf("relation limit exceeded: %d", b.maxRelations)
	}
	b.relations[identity] = relation
	return nil
}

func occurrenceLocation(path string, occurrence *scippb.Occurrence, enclosing bool) codegraph.Location {
	values := occurrence.GetRange()
	if enclosing {
		values = occurrence.GetEnclosingRange()
	}
	if single := occurrence.GetSingleLineRange(); single != nil && !enclosing {
		return location(path, single.GetLine(), single.GetStartCharacter(), single.GetLine(), single.GetEndCharacter())
	}
	if multi := occurrence.GetMultiLineRange(); multi != nil && !enclosing {
		return location(path, multi.GetStartLine(), multi.GetStartCharacter(), multi.GetEndLine(), multi.GetEndCharacter())
	}
	if single := occurrence.GetSingleLineEnclosingRange(); single != nil && enclosing {
		return location(path, single.GetLine(), single.GetStartCharacter(), single.GetLine(), single.GetEndCharacter())
	}
	if multi := occurrence.GetMultiLineEnclosingRange(); multi != nil && enclosing {
		return location(path, multi.GetStartLine(), multi.GetStartCharacter(), multi.GetEndLine(), multi.GetEndCharacter())
	}
	if len(values) == 3 {
		return location(path, values[0], values[1], values[0], values[2])
	}
	if len(values) == 4 {
		return location(path, values[0], values[1], values[2], values[3])
	}
	if enclosing {
		return occurrenceLocation(path, occurrence, false)
	}
	return codegraph.Location{FilePath: path}
}

func location(path string, startLine, startColumn, endLine, endColumn int32) codegraph.Location {
	return codegraph.Location{
		FilePath: path,
		Start:    codegraph.Position{Line: int(startLine) + 1, Column: int(startColumn) + 1},
		End:      codegraph.Position{Line: int(endLine) + 1, Column: int(endColumn) + 1},
	}
}

func enclosingDefinition(definitions []definition, location codegraph.Location) *definition {
	var best *definition
	for index := range definitions {
		candidate := &definitions[index]
		if !contains(candidate.range_, location) {
			continue
		}
		if best == nil || span(candidate.range_) < span(best.range_) {
			best = candidate
		}
	}
	return best
}

func contains(outer, inner codegraph.Location) bool {
	return comparePosition(outer.Start, inner.Start) <= 0 && comparePosition(outer.End, inner.End) >= 0
}

func comparePosition(left, right codegraph.Position) int {
	if left.Line != right.Line {
		return left.Line - right.Line
	}
	return left.Column - right.Column
}

func span(value codegraph.Location) int64 {
	return int64(value.End.Line-value.Start.Line)*1_000_000 + int64(value.End.Column-value.Start.Column)
}

func hasRole(occurrence *scippb.Occurrence, role scippb.SymbolRole) bool {
	return occurrence.GetSymbolRoles()&int32(role) != 0
}

func entityKind(kind scippb.SymbolInformation_Kind) (codegraph.EntityKind, bool) {
	switch kind {
	case scippb.SymbolInformation_Class, scippb.SymbolInformation_Struct, scippb.SymbolInformation_Enum,
		scippb.SymbolInformation_Object, scippb.SymbolInformation_Type, scippb.SymbolInformation_TypeAlias,
		scippb.SymbolInformation_Union, scippb.SymbolInformation_Message:
		return codegraph.EntityType, true
	case scippb.SymbolInformation_Interface, scippb.SymbolInformation_Trait, scippb.SymbolInformation_Protocol,
		scippb.SymbolInformation_TypeClass:
		return codegraph.EntityInterface, true
	case scippb.SymbolInformation_Function, scippb.SymbolInformation_Constructor:
		return codegraph.EntityFunction, true
	case scippb.SymbolInformation_Method, scippb.SymbolInformation_AbstractMethod, scippb.SymbolInformation_StaticMethod,
		scippb.SymbolInformation_TraitMethod, scippb.SymbolInformation_ProtocolMethod, scippb.SymbolInformation_TypeClassMethod:
		return codegraph.EntityMethod, true
	case scippb.SymbolInformation_Field, scippb.SymbolInformation_Property, scippb.SymbolInformation_StaticField,
		scippb.SymbolInformation_StaticProperty, scippb.SymbolInformation_EnumMember:
		return codegraph.EntityField, true
	case scippb.SymbolInformation_Constant:
		return codegraph.EntityConstant, true
	case scippb.SymbolInformation_Variable, scippb.SymbolInformation_StaticVariable, scippb.SymbolInformation_Value:
		return codegraph.EntityVariable, true
	case scippb.SymbolInformation_Module, scippb.SymbolInformation_Namespace:
		return codegraph.EntityModule, true
	case scippb.SymbolInformation_Package, scippb.SymbolInformation_PackageObject:
		return codegraph.EntityPackage, true
	default:
		return "", false
	}
}

func inferredKind(kind scippb.SyntaxKind) (codegraph.EntityKind, bool) {
	switch kind {
	case scippb.SyntaxKind_IdentifierFunction, scippb.SyntaxKind_IdentifierFunctionDefinition:
		return codegraph.EntityFunction, true
	case scippb.SyntaxKind_IdentifierType, scippb.SyntaxKind_IdentifierBuiltinType:
		return codegraph.EntityType, true
	case scippb.SyntaxKind_IdentifierNamespace:
		return codegraph.EntityPackage, true
	case scippb.SyntaxKind_IdentifierConstant:
		return codegraph.EntityConstant, true
	case scippb.SyntaxKind_IdentifierMutableGlobal:
		return codegraph.EntityVariable, true
	default:
		return "", false
	}
}

func normalizeLanguage(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "typescript", "tsx":
		return "typescript"
	case "javascript", "jsx":
		return "javascript"
	case "java":
		return "java"
	case "kotlin":
		return "kotlin"
	case "python":
		return "python"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func safeRelativePath(value string) (string, error) {
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "../") || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("unsafe relative path %q", value)
	}
	return cleaned, nil
}

func displayName(info *scippb.SymbolInformation) string {
	if value := strings.TrimSpace(info.GetDisplayName()); value != "" {
		return value
	}
	return symbolTail(info.GetSymbol())
}

func symbolTail(symbol string) string {
	value := strings.TrimSpace(symbol)
	value = strings.TrimRight(value, "#.)/[]")
	parts := strings.FieldsFunc(value, func(character rune) bool {
		return character == '/' || character == '#' || character == '.' || character == ' '
	})
	if len(parts) == 0 {
		return symbol
	}
	return parts[len(parts)-1]
}

func signatureText(info *scippb.SymbolInformation) string {
	if info.GetSignatureDocumentation() == nil {
		return ""
	}
	return strings.TrimSpace(info.GetSignatureDocumentation().GetText())
}

func isTestName(name, path string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	path = strings.ToLower(filepath.ToSlash(path))
	return strings.HasPrefix(name, "test") || strings.HasSuffix(path, "_test.py") ||
		strings.Contains(path, "/test/") || strings.Contains(path, "/tests/") ||
		strings.HasSuffix(path, ".test.ts") || strings.HasSuffix(path, ".test.tsx") ||
		strings.HasSuffix(path, ".spec.ts") || strings.HasSuffix(path, "test.java") || strings.HasSuffix(path, "test.kt")
}

func positiveOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func sortedIndexFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".scip") {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}
