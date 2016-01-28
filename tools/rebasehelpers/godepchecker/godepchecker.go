package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/openshift/origin/tools/rebasehelpers/util"
)

var gopath = os.Getenv("GOPATH")

func main() {

	fmt.Println(`
  Assumes the following:
  - $GOPATH is set to a single directory (not the godepsified path)
  - "godeps save ./..." has not yet been run on origin
  - The desired level of kubernetes is checked out
`)
	var self, other string
	flag.StringVar(&self, "self", filepath.Join(gopath, "src/github.com/openshift/origin/Godeps/Godeps.json"), "The first file to compare")
	flag.StringVar(&other, "other", filepath.Join(gopath, "src/k8s.io/kubernetes/Godeps/Godeps.json"), "The other file to compare")
	flag.Parse()

	// List packages imported by origin Godeps
	originGodeps, err := loadGodeps(self)
	if err != nil {
		exit(fmt.Sprintf("Error loading %s:", self), err)
	}

	// List packages imported by kubernetes Godeps
	k8sGodeps, err := loadGodeps(other)
	if err != nil {
		exit(fmt.Sprintf("Error loading %s:", other), err)
	}

	// List packages imported by origin
	_, errs := loadImports(".")
	if len(errs) > 0 {
		exit("Error loading imports:", errs...)
	}

	mine := []string{}
	yours := []string{}
	ours := []string{}
	for k := range originGodeps {
		if _, exists := k8sGodeps[k]; exists {
			ours = append(ours, k)
		} else {
			mine = append(mine, k)
		}
	}
	for k := range k8sGodeps {
		if _, exists := originGodeps[k]; !exists {
			yours = append(yours, k)
		}
	}

	sort.Strings(mine)
	sort.Strings(yours)
	sort.Strings(ours)

	// Check for missing k8s deps
	if len(yours) > 0 {
		fmt.Println("k8s-only godep imports (may need adding to origin):")
		for _, k := range yours {
			fmt.Println(k)
		}
		fmt.Printf("\n\n\n")
	}

	// Check `mine` for unused local deps (might be used transitively by other Godeps)

	// Check `ours` for different levels
	for _, k := range ours {
		if oRev, kRev := originGodeps[k].Rev, k8sGodeps[k].Rev; oRev != kRev {
			fmt.Printf("Mismatch on %s:\n", k)
			if older, err := util.IsAncestor(oRev, kRev, filepath.Join(gopath, "src", k)); older && err == nil {
				fmt.Printf("    Origin: %s (older)\n", oRev)
				fmt.Printf("    K8s:    %s (newer)\n", kRev)
			} else if newer, err := util.IsAncestor(kRev, oRev, filepath.Join(gopath, "src", k)); newer && err == nil {
				fmt.Printf("    Origin: %s (newer)\n", oRev)
				fmt.Printf("    K8s:    %s (older)\n", kRev)
			} else {
				fmt.Printf("    Origin: %s (unknown)\n", oRev)
				fmt.Printf("    K8s:    %s (unknown)\n", kRev)
				fmt.Printf("    %s\n", err)
			}
		}
	}
}

func exit(reason string, errors ...error) {
	fmt.Fprintf(os.Stderr, "%s\n", reason)
	for _, err := range errors {
		fmt.Fprintln(os.Stderr, err.Error())
	}
	os.Exit(2)
}

func loadImports(root string) (map[string]bool, []error) {
	imports := map[string]bool{}
	errs := []error{}
	fset := &token.FileSet{}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		// Don't walk godeps
		if info.Name() == "Godeps" && info.IsDir() {
			return filepath.SkipDir
		}

		if strings.HasSuffix(info.Name(), ".go") && info.Mode().IsRegular() {
			if fileAST, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly); err != nil {
				errs = append(errs, err)
			} else {
				for i := range fileAST.Imports {
					pkg := fileAST.Imports[i].Path.Value
					imports[pkg[1:len(pkg)-2]] = true
				}
			}
		}
		return nil
	})
	return imports, errs
}

type Godep struct {
	Deps []Dep
}
type Dep struct {
	ImportPath string
	Comment    string
	Rev        string
}

func loadGodeps(file string) (map[string]Dep, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}
	godeps := &Godep{}
	if err := json.Unmarshal(data, godeps); err != nil {
		return nil, err
	}

	depmap := map[string]Dep{}
	for i := range godeps.Deps {
		dep := godeps.Deps[i]
		if _, exists := depmap[dep.ImportPath]; exists {
			return nil, fmt.Errorf("imports %q multiple times", dep.ImportPath)
		}
		depmap[dep.ImportPath] = dep
	}
	return depmap, nil
}
