// SPDX-License-Identifier: MPL-2.0

// Package golanganalyzer provides headless, build-aware extraction for Go
// repositories. It uses Go's package, syntax, and type information directly;
// it does not expose an editor command server or execute repository code.
package golanganalyzer

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
	"golang.org/x/tools/go/packages"
)

const (
	analyzerName    = "go-packages"
	analyzerVersion = "1.0.0"
	defaultMaxFiles = 5000
	defaultMaxNodes = 200000
	defaultMaxEdges = 1000000
)

type Analyzer struct{}

func New() *Analyzer              { return &Analyzer{} }
func (*Analyzer) Name() string    { return analyzerName }
func (*Analyzer) Version() string { return analyzerVersion }

type builder struct {
	root         string
	entities     map[string]codegraph.Entity
	relations    map[string]codegraph.Relation
	files        map[string]struct{}
	maxFiles     int
	maxEntities  int
	maxRelations int
	err          error
	types        []typeCandidate
}

type typeCandidate struct {
	key           string
	named         *types.Named
	interfaceType *types.Interface
}

func (a *Analyzer) Analyze(ctx context.Context, request codegraph.Request) (codegraph.Snapshot, error) {
	started := time.Now().UTC()
	root, err := filepath.Abs(strings.TrimSpace(request.RepositoryPath))
	if err != nil {
		return codegraph.Snapshot{}, fmt.Errorf("resolve repository path: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return codegraph.Snapshot{}, fmt.Errorf("stat repository path: %w", err)
	}
	if !info.IsDir() {
		return codegraph.Snapshot{}, errors.New("repository path must be a directory")
	}
	revision, branch, dirty, err := codegraph.ResolveRepositoryState(ctx, root, request.Revision, request.Branch)
	if err != nil {
		return codegraph.Snapshot{}, err
	}
	if dirty && !request.AllowDirty {
		return codegraph.Snapshot{}, errors.New("repository has uncommitted changes; commit them or explicitly allow a dirty analysis")
	}

	b := &builder{
		root: root, entities: map[string]codegraph.Entity{}, relations: map[string]codegraph.Relation{}, files: map[string]struct{}{},
		maxFiles: positiveOr(request.MaxFiles, defaultMaxFiles), maxEntities: positiveOr(request.MaxEntities, defaultMaxNodes),
		maxRelations: positiveOr(request.MaxRelations, defaultMaxEdges),
	}
	repositoryName := strings.TrimSpace(request.RepositoryName)
	if repositoryName == "" {
		repositoryName = filepath.Base(root)
	}
	repositoryKey := codegraph.StableKey("go", codegraph.EntityRepository, ".")
	b.addEntity(codegraph.Entity{
		Key: repositoryKey, Language: "go", Kind: codegraph.EntityRepository,
		Name: repositoryName, QualifiedName: ".", Metadata: map[string]string{
			"path": root, "repository": repositoryName, "branch": branch, "revision": revision,
		},
	})

	modules, err := discoverModules(root)
	if err != nil {
		return codegraph.Snapshot{}, err
	}
	for _, moduleDir := range modules {
		if err := b.analyzeModule(ctx, repositoryKey, moduleDir); err != nil {
			return codegraph.Snapshot{}, err
		}
	}
	if b.err != nil {
		return codegraph.Snapshot{}, b.err
	}
	b.addImplementationRelations()
	if b.err != nil {
		return codegraph.Snapshot{}, b.err
	}

	entities := make([]codegraph.Entity, 0, len(b.entities))
	for _, entity := range b.entities {
		entities = append(entities, entity)
	}
	relations := make([]codegraph.Relation, 0, len(b.relations))
	for _, relation := range b.relations {
		relations = append(relations, relation)
	}
	if dirty {
		revision += "+worktree." + codegraph.WorktreeFingerprint(entities)
	}
	for index := range entities {
		if entities[index].Kind == codegraph.EntityRepository {
			entities[index].Metadata["revision"] = revision
		}
	}
	snapshot := codegraph.Snapshot{
		RepositoryPath: root, RepositoryName: repositoryName, Branch: branch, Revision: revision,
		Analyzer: a.Name(), AnalyzerVersion: a.Version(),
		StartedAt: started, CompletedAt: time.Now().UTC(), Entities: entities, Relations: relations,
		Statistics: map[string]int64{"files": int64(len(b.files)), "modules": int64(len(modules))},
	}
	if err := snapshot.Normalize(); err != nil {
		return codegraph.Snapshot{}, fmt.Errorf("normalize code graph: %w", err)
	}
	return snapshot, nil
}

func (b *builder) analyzeModule(ctx context.Context, repositoryKey, moduleDir string) error {
	cfg := &packages.Config{
		Context: ctx,
		Dir:     moduleDir,
		Tests:   true,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
			packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return fmt.Errorf("load Go packages in %s: %w", moduleDir, err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		return fmt.Errorf("Go package loading reported errors in %s", moduleDir)
	}
	moduleName := filepath.Base(moduleDir)
	for _, pkg := range pkgs {
		if pkg.Module != nil && pkg.Module.Path != "" {
			moduleName = pkg.Module.Path
			break
		}
	}
	moduleKey := codegraph.StableKey("go", codegraph.EntityModule, moduleName)
	b.addEntity(codegraph.Entity{Key: moduleKey, Language: "go", Kind: codegraph.EntityModule, Name: filepath.Base(moduleName), QualifiedName: moduleName})
	b.addRelation(codegraph.Relation{SourceKey: repositoryKey, TargetKey: moduleKey, Kind: codegraph.RelationContains, Confidence: 1})

	seenPackages := map[string]struct{}{}
	for _, pkg := range pkgs {
		if pkg.PkgPath == "" || len(pkg.Syntax) == 0 || !packageInsideRoot(pkg, b.root) {
			continue
		}
		packageKey := packageEntityKey(pkg.PkgPath)
		if _, seen := seenPackages[packageKey]; !seen {
			b.addEntity(codegraph.Entity{Key: packageKey, Language: "go", Kind: codegraph.EntityPackage, Name: pkg.Name, QualifiedName: pkg.PkgPath})
			b.addRelation(codegraph.Relation{SourceKey: moduleKey, TargetKey: packageKey, Kind: codegraph.RelationContains, Confidence: 1})
			seenPackages[packageKey] = struct{}{}
		}
		for index, file := range pkg.Syntax {
			filename := pkg.Fset.Position(file.Pos()).Filename
			if filename == "" && index < len(pkg.CompiledGoFiles) {
				filename = pkg.CompiledGoFiles[index]
			}
			relative, ok := b.relativePath(filename)
			if !ok {
				continue
			}
			b.files[relative] = struct{}{}
			if len(b.files) > b.maxFiles {
				return fmt.Errorf("file limit exceeded: %d", b.maxFiles)
			}
			contents, err := os.ReadFile(filename)
			if err != nil {
				return fmt.Errorf("read %s: %w", filename, err)
			}
			fileKey := codegraph.StableKey("go", codegraph.EntityFile, relative)
			b.addEntity(codegraph.Entity{
				Key: fileKey, Language: "go", Kind: codegraph.EntityFile, Name: filepath.Base(relative), QualifiedName: relative,
				ContentHash: codegraph.HashContent(contents), Location: location(pkg.Fset, file.Pos(), file.End(), relative),
			})
			b.addRelation(codegraph.Relation{SourceKey: packageKey, TargetKey: fileKey, Kind: codegraph.RelationContains, Confidence: 1})
			b.addImports(pkg, file, fileKey, relative)
			b.addDeclarations(pkg, file, fileKey, relative, contents)
		}
	}
	return b.err
}

func (b *builder) addImports(pkg *packages.Package, file *ast.File, fileKey, relative string) {
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path == "" {
			continue
		}
		targetKey := packageEntityKey(path)
		name := filepath.Base(path)
		if imported := pkg.Imports[path]; imported != nil && imported.Name != "" {
			name = imported.Name
		}
		b.addEntity(codegraph.Entity{
			Key: targetKey, Language: "go", Kind: codegraph.EntityPackage, Name: name, QualifiedName: path,
			Metadata: map[string]string{"external": strconv.FormatBool(!strings.HasPrefix(path, modulePath(pkg)))},
		})
		b.addRelation(codegraph.Relation{
			SourceKey: fileKey, TargetKey: targetKey, Kind: codegraph.RelationImports, Evidence: spec.Path.Value,
			Confidence: 1, Location: location(pkg.Fset, spec.Pos(), spec.End(), relative),
		})
	}
}

func (b *builder) addDeclarations(pkg *packages.Package, file *ast.File, fileKey, relative string, contents []byte) {
	for _, decl := range file.Decls {
		switch value := decl.(type) {
		case *ast.FuncDecl:
			entity, object, ok := entityForDefinition(pkg, value.Name, relative, value, contents)
			if !ok {
				continue
			}
			entity = withDocumentation(entity, value.Doc)
			if isTestFunction(value.Name.Name, relative) {
				entity.Kind = codegraph.EntityTest
				entity.Key = codegraph.StableKey("go", entity.Kind, entity.QualifiedName)
			}
			b.addEntity(entity)
			b.addRelation(codegraph.Relation{SourceKey: fileKey, TargetKey: entity.Key, Kind: codegraph.RelationDefines, Confidence: 1, Location: entity.Location})
			if receiverKey := receiverEntityKey(object); receiverKey != "" {
				b.ensureObjectEntity(object.Type().(*types.Signature).Recv().Type())
				b.addRelation(codegraph.Relation{SourceKey: receiverKey, TargetKey: entity.Key, Kind: codegraph.RelationDefines, Confidence: 1, Location: entity.Location})
			}
			b.addUses(pkg, value.Body, entity, relative)
		case *ast.GenDecl:
			for _, spec := range value.Specs {
				switch typed := spec.(type) {
				case *ast.TypeSpec:
					entity, object, ok := entityForDefinition(pkg, typed.Name, relative, typed, contents)
					if !ok {
						continue
					}
					entity = withDocumentation(entity, firstComment(typed.Doc, value.Doc))
					b.addEntity(entity)
					b.addRelation(codegraph.Relation{SourceKey: fileKey, TargetKey: entity.Key, Kind: codegraph.RelationDefines, Confidence: 1, Location: entity.Location})
					if named, ok := object.Type().(*types.Named); ok {
						candidate := typeCandidate{key: entity.Key, named: named}
						if iface, ok := named.Underlying().(*types.Interface); ok {
							candidate.interfaceType = iface.Complete()
						}
						b.types = append(b.types, candidate)
					}
					b.addTypeMembers(pkg, typed, entity.Key, relative, contents)
				case *ast.ValueSpec:
					for _, name := range typed.Names {
						entity, _, ok := entityForDefinition(pkg, name, relative, typed, contents)
						if !ok {
							continue
						}
						entity = withDocumentation(entity, firstComment(typed.Doc, value.Doc))
						b.addEntity(entity)
						b.addRelation(codegraph.Relation{SourceKey: fileKey, TargetKey: entity.Key, Kind: codegraph.RelationDefines, Confidence: 1, Location: entity.Location})
					}
				}
			}
		}
	}
}

func (b *builder) addTypeMembers(pkg *packages.Package, spec *ast.TypeSpec, ownerKey, relative string, contents []byte) {
	var fields *ast.FieldList
	switch typed := spec.Type.(type) {
	case *ast.StructType:
		fields = typed.Fields
	case *ast.InterfaceType:
		fields = typed.Methods
	}
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			target := typeObjectFromExpression(pkg.TypesInfo, field.Type)
			if target != nil {
				targetEntity, ok := b.ensureObject(target)
				if ok {
					b.addRelation(codegraph.Relation{
						SourceKey: ownerKey, TargetKey: targetEntity.Key, Kind: codegraph.RelationEmbeds,
						Confidence: 1, Location: location(pkg.Fset, field.Pos(), field.End(), relative),
					})
				}
			}
			continue
		}
		for _, name := range field.Names {
			entity, _, ok := entityForDefinition(pkg, name, relative, field, contents)
			if !ok {
				continue
			}
			entity = withDocumentation(entity, field.Doc)
			owner := b.entities[ownerKey]
			entity.QualifiedName = owner.QualifiedName + "." + name.Name
			if _, ok := field.Type.(*ast.FuncType); ok {
				entity.Kind = codegraph.EntityMethod
			} else {
				entity.Kind = codegraph.EntityField
			}
			entity.Key = codegraph.StableKey("go", entity.Kind, entity.QualifiedName)
			b.addEntity(entity)
			b.addRelation(codegraph.Relation{SourceKey: ownerKey, TargetKey: entity.Key, Kind: codegraph.RelationDefines, Confidence: 1, Location: entity.Location})
		}
	}
}

func firstComment(primary, fallback *ast.CommentGroup) *ast.CommentGroup {
	if primary != nil {
		return primary
	}
	return fallback
}

func withDocumentation(entity codegraph.Entity, comments *ast.CommentGroup) codegraph.Entity {
	if comments == nil {
		return entity
	}
	documentation := strings.TrimSpace(comments.Text())
	if documentation == "" {
		return entity
	}
	if entity.Metadata == nil {
		entity.Metadata = map[string]string{}
	}
	entity.Metadata["documentation"] = documentation
	return entity
}

func (b *builder) addUses(pkg *packages.Package, body *ast.BlockStmt, source codegraph.Entity, relative string) {
	if body == nil {
		return
	}
	called := map[token.Pos]struct{}{}
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		object, position := calledObject(pkg.TypesInfo, call.Fun)
		if object == nil {
			return true
		}
		target, ok := b.ensureObject(object)
		if !ok {
			return true
		}
		called[position] = struct{}{}
		kind := codegraph.RelationCalls
		if source.Kind == codegraph.EntityTest {
			kind = codegraph.RelationTests
		}
		b.addRelation(codegraph.Relation{
			SourceKey: source.Key, TargetKey: target.Key, Kind: kind, Evidence: target.QualifiedName,
			Confidence: 1, Location: location(pkg.Fset, call.Pos(), call.End(), relative),
		})
		return true
	})
	ast.Inspect(body, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if _, ok := called[identifier.Pos()]; ok {
			return true
		}
		object := pkg.TypesInfo.Uses[identifier]
		if !referenceable(object, pkg.Types) {
			return true
		}
		target, ok := b.ensureObject(object)
		if !ok || target.Key == source.Key {
			return true
		}
		b.addRelation(codegraph.Relation{
			SourceKey: source.Key, TargetKey: target.Key, Kind: codegraph.RelationReferences,
			Evidence: target.QualifiedName, Confidence: 1,
			Location: location(pkg.Fset, identifier.Pos(), identifier.End(), relative),
		})
		return true
	})
}

func (b *builder) addImplementationRelations() {
	for _, concrete := range b.types {
		if concrete.interfaceType != nil {
			continue
		}
		for _, candidate := range b.types {
			iface := candidate.interfaceType
			if iface == nil || iface.NumMethods() == 0 || concrete.key == candidate.key {
				continue
			}
			if types.Implements(concrete.named, iface) || types.Implements(types.NewPointer(concrete.named), iface) {
				b.addRelation(codegraph.Relation{
					SourceKey: concrete.key, TargetKey: candidate.key, Kind: codegraph.RelationImplements,
					Evidence: "go/types method-set compatibility", Confidence: 1,
				})
			}
		}
	}
}

func (b *builder) ensureObject(object types.Object) (codegraph.Entity, bool) {
	if object == nil || object.Pkg() == nil {
		return codegraph.Entity{}, false
	}
	qualified, kind := qualifiedObject(object)
	if qualified == "" || kind == "" {
		return codegraph.Entity{}, false
	}
	entity := codegraph.Entity{
		Key: codegraph.StableKey("go", kind, qualified), Language: "go", Kind: kind,
		Name: object.Name(), QualifiedName: qualified, Signature: types.ObjectString(object, qualifier),
		Metadata: map[string]string{"external": "true"},
	}
	b.addEntity(entity)
	packageKey := packageEntityKey(object.Pkg().Path())
	b.addEntity(codegraph.Entity{
		Key: packageKey, Language: "go", Kind: codegraph.EntityPackage, Name: object.Pkg().Name(),
		QualifiedName: object.Pkg().Path(), Metadata: map[string]string{"external": "true"},
	})
	return b.entities[entity.Key], true
}

func (b *builder) ensureObjectEntity(value types.Type) {
	named := namedType(value)
	if named != nil && named.Obj() != nil {
		_, _ = b.ensureObject(named.Obj())
	}
}

func (b *builder) addEntity(entity codegraph.Entity) {
	if b.err != nil {
		return
	}
	if existing, ok := b.entities[entity.Key]; ok {
		if existing.Location.FilePath == "" && entity.Location.FilePath != "" {
			b.entities[entity.Key] = entity
		}
		return
	}
	if len(b.entities) >= b.maxEntities {
		b.err = fmt.Errorf("entity limit exceeded: %d", b.maxEntities)
		return
	}
	b.entities[entity.Key] = entity
}

func (b *builder) addRelation(relation codegraph.Relation) {
	if b.err != nil {
		return
	}
	if relation.Confidence == 0 {
		relation.Confidence = 1
	}
	key := relation.SourceKey + "\x00" + string(relation.Kind) + "\x00" + relation.TargetKey + "\x00" +
		relation.Location.FilePath + fmt.Sprint("\x00", relation.Location.Start.Line, ":", relation.Location.Start.Column)
	if _, exists := b.relations[key]; exists {
		return
	}
	if len(b.relations) >= b.maxRelations {
		b.err = fmt.Errorf("relation limit exceeded: %d", b.maxRelations)
		return
	}
	b.relations[key] = relation
}

func (b *builder) relativePath(filename string) (string, bool) {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(b.root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

func discoverModules(root string) ([]string, error) {
	modules := make([]string, 0, 1)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != root {
			switch entry.Name() {
			case ".git", "vendor", "node_modules", ".idea", ".vscode":
				return filepath.SkipDir
			}
		}
		if !entry.IsDir() && entry.Name() == "go.mod" {
			modules = append(modules, filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover Go modules: %w", err)
	}
	if len(modules) == 0 {
		return nil, errors.New("no go.mod found in repository")
	}
	sort.Strings(modules)
	return modules, nil
}

func entityForDefinition(pkg *packages.Package, identifier *ast.Ident, relative string, node ast.Node, contents []byte) (codegraph.Entity, types.Object, bool) {
	object := pkg.TypesInfo.Defs[identifier]
	if object == nil || object.Pkg() == nil {
		return codegraph.Entity{}, nil, false
	}
	qualified, kind := qualifiedObject(object)
	if qualified == "" || kind == "" {
		return codegraph.Entity{}, nil, false
	}
	loc := location(pkg.Fset, node.Pos(), node.End(), relative)
	entity := codegraph.Entity{
		Key: codegraph.StableKey("go", kind, qualified), Language: "go", Kind: kind, Name: identifier.Name,
		QualifiedName: qualified, Signature: types.ObjectString(object, qualifier), Location: loc,
		ContentHash: hashRange(pkg.Fset, node.Pos(), node.End(), contents),
	}
	return entity, object, true
}

func qualifiedObject(object types.Object) (string, codegraph.EntityKind) {
	if object == nil || object.Pkg() == nil {
		return "", ""
	}
	prefix := object.Pkg().Path() + "."
	switch value := object.(type) {
	case *types.TypeName:
		if _, ok := value.Type().Underlying().(*types.Interface); ok {
			return prefix + value.Name(), codegraph.EntityInterface
		}
		return prefix + value.Name(), codegraph.EntityType
	case *types.Func:
		signature, _ := value.Type().(*types.Signature)
		if signature != nil && signature.Recv() != nil {
			receiver := namedType(signature.Recv().Type())
			if receiver != nil && receiver.Obj() != nil {
				return prefix + receiver.Obj().Name() + "." + value.Name(), codegraph.EntityMethod
			}
		}
		return prefix + value.Name(), codegraph.EntityFunction
	case *types.Const:
		return prefix + value.Name(), codegraph.EntityConstant
	case *types.Var:
		if value.IsField() {
			return prefix + value.Name(), codegraph.EntityField
		}
		if value.Parent() == value.Pkg().Scope() {
			return prefix + value.Name(), codegraph.EntityVariable
		}
	}
	return "", ""
}

func receiverEntityKey(object types.Object) string {
	function, ok := object.(*types.Func)
	if !ok {
		return ""
	}
	signature, _ := function.Type().(*types.Signature)
	if signature == nil || signature.Recv() == nil {
		return ""
	}
	named := namedType(signature.Recv().Type())
	if named == nil || named.Obj() == nil || named.Obj().Pkg() == nil {
		return ""
	}
	qualified := named.Obj().Pkg().Path() + "." + named.Obj().Name()
	kind := codegraph.EntityType
	if _, ok := named.Underlying().(*types.Interface); ok {
		kind = codegraph.EntityInterface
	}
	return codegraph.StableKey("go", kind, qualified)
}

func calledObject(info *types.Info, expression ast.Expr) (types.Object, token.Pos) {
	switch value := expression.(type) {
	case *ast.Ident:
		return info.Uses[value], value.Pos()
	case *ast.SelectorExpr:
		if selection := info.Selections[value]; selection != nil {
			return selection.Obj(), value.Sel.Pos()
		}
		return info.Uses[value.Sel], value.Sel.Pos()
	case *ast.IndexExpr:
		return calledObject(info, value.X)
	case *ast.IndexListExpr:
		return calledObject(info, value.X)
	case *ast.ParenExpr:
		return calledObject(info, value.X)
	}
	return nil, token.NoPos
}

func referenceable(object types.Object, currentPackage *types.Package) bool {
	if object == nil || object.Pkg() == nil {
		return false
	}
	switch value := object.(type) {
	case *types.Func, *types.TypeName, *types.Const:
		return true
	case *types.Var:
		return value.IsField() || value.Parent() == value.Pkg().Scope() || value.Pkg() != currentPackage
	default:
		return false
	}
}

func typeObjectFromExpression(info *types.Info, expression ast.Expr) types.Object {
	switch value := expression.(type) {
	case *ast.Ident:
		if object := info.Uses[value]; object != nil {
			return object
		}
		return info.Defs[value]
	case *ast.SelectorExpr:
		return info.Uses[value.Sel]
	case *ast.StarExpr:
		return typeObjectFromExpression(info, value.X)
	case *ast.IndexExpr:
		return typeObjectFromExpression(info, value.X)
	case *ast.IndexListExpr:
		return typeObjectFromExpression(info, value.X)
	}
	return nil
}

func location(files *token.FileSet, start, end token.Pos, relative string) codegraph.Location {
	startPosition := files.PositionFor(start, true)
	endPosition := files.PositionFor(end, true)
	return codegraph.Location{
		FilePath: relative,
		Start:    codegraph.Position{Line: startPosition.Line, Column: startPosition.Column},
		End:      codegraph.Position{Line: endPosition.Line, Column: endPosition.Column},
	}
}

func hashRange(files *token.FileSet, start, end token.Pos, contents []byte) string {
	startPosition := files.PositionFor(start, true)
	endPosition := files.PositionFor(end, true)
	if startPosition.Offset < 0 || endPosition.Offset < startPosition.Offset || endPosition.Offset > len(contents) {
		return ""
	}
	return codegraph.HashContent(contents[startPosition.Offset:endPosition.Offset])
}

func namedType(value types.Type) *types.Named {
	switch typed := value.(type) {
	case *types.Named:
		return typed
	case *types.Pointer:
		if named, ok := typed.Elem().(*types.Named); ok {
			return named
		}
	}
	return nil
}

func packageInsideRoot(pkg *packages.Package, root string) bool {
	for _, filename := range pkg.CompiledGoFiles {
		absolute, err := filepath.Abs(filename)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(root, absolute)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func packageEntityKey(path string) string {
	return codegraph.StableKey("go", codegraph.EntityPackage, path)
}

func modulePath(pkg *packages.Package) string {
	if pkg.Module != nil {
		return pkg.Module.Path
	}
	return ""
}

func qualifier(pkg *types.Package) string { return pkg.Name() }

func isTestFunction(name, relative string) bool {
	if !strings.HasSuffix(relative, "_test.go") {
		return false
	}
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") || strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Example")
}

func positiveOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
