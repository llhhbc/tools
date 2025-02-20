// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"fmt"
	"go/ast"
	"go/scanner"
	"go/types"
	"sort"

	"golang.org/x/mod/module"
	"golang.org/x/tools/gopls/internal/lsp/source"
	"golang.org/x/tools/gopls/internal/span"
	"golang.org/x/tools/internal/memoize"
)

// pkg contains the type information needed by the source package.
type pkg struct {
	m               *Metadata
	mode            source.ParseMode
	goFiles         []*source.ParsedGoFile
	compiledGoFiles []*source.ParsedGoFile
	diagnostics     []*source.Diagnostic
	deps            map[PackageID]*pkg // use m.DepsBy{Pkg,Imp}Path to look up ID
	version         *module.Version
	parseErrors     []scanner.ErrorList
	typeErrors      []types.Error
	types           *types.Package
	typesInfo       *types.Info
	typesSizes      types.Sizes
	hasFixedFiles   bool // if true, AST was sufficiently mangled that we should hide type errors

	analyses memoize.Store // maps analyzer.Name to Promise[actionResult]
}

// A loadScope defines a package loading scope for use with go/packages.
type loadScope interface {
	aScope()
}

type (
	fileLoadScope    span.URI // load packages containing a file (including command-line-arguments)
	packageLoadScope string   // load a specific package
	moduleLoadScope  string   // load packages in a specific module
	viewLoadScope    span.URI // load the workspace
)

// Implement the loadScope interface.
func (fileLoadScope) aScope()    {}
func (packageLoadScope) aScope() {}
func (moduleLoadScope) aScope()  {}
func (viewLoadScope) aScope()    {}

func (p *pkg) ID() string {
	return string(p.m.ID)
}

func (p *pkg) Name() string {
	return string(p.m.Name)
}

func (p *pkg) PkgPath() string {
	return string(p.m.PkgPath)
}

func (p *pkg) ParseMode() source.ParseMode {
	return p.mode
}

func (p *pkg) CompiledGoFiles() []*source.ParsedGoFile {
	return p.compiledGoFiles
}

func (p *pkg) File(uri span.URI) (*source.ParsedGoFile, error) {
	for _, cgf := range p.compiledGoFiles {
		if cgf.URI == uri {
			return cgf, nil
		}
	}
	for _, gf := range p.goFiles {
		if gf.URI == uri {
			return gf, nil
		}
	}
	return nil, fmt.Errorf("no parsed file for %s in %v", uri, p.m.ID)
}

func (p *pkg) GetSyntax() []*ast.File {
	var syntax []*ast.File
	for _, pgf := range p.compiledGoFiles {
		syntax = append(syntax, pgf.File)
	}
	return syntax
}

func (p *pkg) GetTypes() *types.Package {
	return p.types
}

func (p *pkg) GetTypesInfo() *types.Info {
	return p.typesInfo
}

func (p *pkg) GetTypesSizes() types.Sizes {
	return p.typesSizes
}

func (p *pkg) ForTest() string {
	return string(p.m.ForTest)
}

// DirectDep returns the directly imported dependency of this package,
// given its PackagePath.  (If you have an ImportPath, e.g. a string
// from an import declaration, use ResolveImportPath instead.
// They may differ in case of vendoring.)
func (p *pkg) DirectDep(pkgPath string) (source.Package, error) {
	if id, ok := p.m.DepsByPkgPath[PackagePath(pkgPath)]; ok {
		if imp := p.deps[id]; imp != nil {
			return imp, nil
		}
	}
	return nil, fmt.Errorf("package does not import package with path %s", pkgPath)
}

// ResolveImportPath returns the directly imported dependency of this package,
// given its ImportPath. See also DirectDep.
func (p *pkg) ResolveImportPath(importPath string) (source.Package, error) {
	if id, ok := p.m.DepsByImpPath[ImportPath(importPath)]; ok && id != "" {
		if imp := p.deps[id]; imp != nil {
			return imp, nil
		}
	}
	return nil, fmt.Errorf("package does not import %s", importPath)
}

func (p *pkg) MissingDependencies() []string {
	// We don't invalidate metadata for import deletions,
	// so check the package imports via the *types.Package.
	//
	// rfindley says: it looks like this is intending to implement
	// a heuristic "if go list couldn't resolve import paths to
	// packages, then probably you're not in GOPATH or a module".
	// This is used to determine if we need to show a warning diagnostic.
	// It looks like this logic is implementing the heuristic that
	// "even if the metadata has a MissingDep, if the types.Package
	// doesn't need that dep anymore we shouldn't show the warning".
	// But either we're outside of GOPATH/Module, or we're not...
	//
	// adonovan says: I think this effectively reverses the
	// heuristic used by the type checker when Importer.Import
	// returns an error---go/types synthesizes a package whose
	// Path is the import path (sans "vendor/")---hence the
	// dubious ImportPath() conversion. A blank DepsByImpPath
	// entry means a missing import.
	//
	// If we invalidate the metadata for import deletions (which
	// should be fast) then we can simply return the blank entries
	// in DepsByImpPath. (They are PackageIDs not PackagePaths,
	// but the caller only cares whether the set is empty!)
	var missing []string
	for _, pkg := range p.types.Imports() {
		if id, ok := p.m.DepsByImpPath[ImportPath(pkg.Path())]; ok && id == "" {
			missing = append(missing, pkg.Path())
		}
	}
	sort.Strings(missing)
	return missing
}

func (p *pkg) Imports() []source.Package {
	var result []source.Package // unordered
	for _, dep := range p.deps {
		result = append(result, dep)
	}
	return result
}

func (p *pkg) Version() *module.Version {
	return p.version
}

func (p *pkg) HasListOrParseErrors() bool {
	return len(p.m.Errors) != 0 || len(p.parseErrors) != 0
}

func (p *pkg) HasTypeErrors() bool {
	return len(p.typeErrors) != 0
}
