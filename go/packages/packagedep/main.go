package main

import (
	"bytes"
	"flag"
	"fmt"
	"golang.org/x/tools/go/packages"
	"log"
	"net/http"
	"os"
	"path"
	"runtime"
)

func IsStandPkg(name string) bool {
	_, err := os.Stat(path.Join(runtime.GOROOT(), "src", name))
	//fmt.Println("check ", path.Join(runtime.GOROOT(), "src", name), err)
	if err == nil {
		return true
	}
	return false
}

var (
	needTest = flag.Bool("needTest", false, "if need test. ")
	anaDir   = flag.String("anaDir", "./cmd/stringer", "analysis dir. ")
	addr     = flag.String("addr", ":9000", "listen addr. ")
	callDeep = flag.Int("deep", 3, "call deep. ")
)

var cfg *packages.Config

func main() {
	flag.Parse()

	cfg = &packages.Config{
		Mode:  packages.NeedName | packages.NeedImports,
		Tests: *needTest,
		//BuildFlags: app.BuildFlags,
	}

	http.HandleFunc("/", handler)

	log.Fatalln(http.ListenAndServe(*addr, nil))
}

func handler(w http.ResponseWriter, r *http.Request) {
	dir := *anaDir

	if r.FormValue("f") != "" {
		dir = r.FormValue("f")
	}

	lpkgs, err := packages.Load(cfg, dir)
	if err != nil {
		w.Write([]byte(fmt.Sprintf("load package of %s failed %v. ", *anaDir, err)))
		w.WriteHeader(500)
		return
	}

	res, err := convertToDot(lpkgs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	img, err := dotToImage("", "svg", res, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.ServeFile(w, r, img)
}

func pkgToNode(p *packages.Package) *dotNode {
	return &dotNode{
		ID: p.PkgPath,
		Attrs: map[string]string{
			"fillcolor": "lightblue",
			"label":     p.Name,
			"penwidth":  "0.5",
		},
	}
}

func convertToDot(lpkgs []*packages.Package) ([]byte, error) {
	if len(lpkgs) == 0 {
		return nil, fmt.Errorf("no package found. ")
	}
	pkg := lpkgs[0]
	if pkg.Errors != nil {
		return nil, fmt.Errorf("get failed %v. ", pkg.Errors)
	}

	cluster, nodes, edges := doPkg(pkg, 0)

	dot := &dotGraph{
		Title:   pkg.PkgPath,
		Minlen:  minlen,
		Cluster: cluster,
		Nodes:   nodes,
		Edges:   edges,
		Options: map[string]string{
			"minlen":    "2",
			"nodesep":   "0.35",
			"nodeshape": "box",
			"nodestyle": "filled,rounded",
			"rankdir":   "LR",
		},
	}

	var buf bytes.Buffer
	if err := dot.WriteDot(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

var seen = make(map[*packages.Package]bool, 0)

func doPkg(pkg *packages.Package, deep int) (*dotCluster, []*dotNode, []*dotEdge) {
	cluster := NewDotCluster(pkg.PkgPath)
	cluster.Attrs = dotAttrs{
		"penwidth":  "0.8",
		"fontsize":  "16",
		"label":     pkg.PkgPath,
		"style":     "filled",
		"fillcolor": "lightyellow",
		"URL":       fmt.Sprintf("/?f=%s", pkg.PkgPath),
		"fontname":  "Tahoma bold",
		"tooltip":   fmt.Sprintf("package: %s", pkg.PkgPath),
		"rank":      "sink",
	}
	cluster.Attrs["bgcolor"] = "#e6ecfa"

	var (
		nodes []*dotNode
		edges []*dotEdge
	)

	lpn := pkgToNode(pkg)
	nodes = append(nodes, pkgToNode(pkg))

	for name, p := range pkg.Imports {
		if IsStandPkg(p.PkgPath) {
			continue
		}

		if seen[p] {
			log.Printf("pkg %#v seen before. \n", p)
			continue
		}
		seen[p] = true
		if deep+1 == *callDeep {
			continue
		}

		c, n, e := doPkg(p, deep+1)
		edges = append(edges, &dotEdge{
			From: lpn,
			To:   n[0],
			Attrs: map[string]string{
				"color": "saddlebrown",
			},
		})
		edges = append(edges, e...)
		cluster.Clusters[name] = c
	}
	if deep > 0 {
		cluster.Nodes = append(cluster.Nodes, lpn)
	}

	return cluster, nodes, edges
}

func aa(pkg *packages.Package) (*dotCluster, []*dotNode, []*dotEdge) {
	cluster := NewDotCluster("focus")
	cluster.Attrs = dotAttrs{
		"bgcolor":   "white",
		"label":     "",
		"labelloc":  "t",
		"labeljust": "c",
		"fontsize":  "18",
	}
	cluster.Attrs["bgcolor"] = "#e6ecfa"
	cluster.Attrs["label"] = pkg.Name

	var (
		nodes []*dotNode
		edges []*dotEdge
	)

	pn := pkgToNode(pkg)
	nodes = append(nodes, pn)

	for name, p := range pkg.Imports {
		if IsStandPkg(p.PkgPath) {
			continue
		}

		n := pkgToNode(p)
		c := NewDotCluster(name)
		c.Nodes = append(c.Nodes, n)
		edges = append(edges, &dotEdge{
			From: pn,
			To:   n,
			Attrs: map[string]string{
				"color": "saddlebrown",
			},
		})
		c.Attrs = dotAttrs{
			"penwidth":  "0.8",
			"fontsize":  "16",
			"label":     name,
			"style":     "filled",
			"fillcolor": "lightyellow",
			"URL":       fmt.Sprintf("/?f=%s", p.PkgPath),
			"fontname":  "Tahoma bold",
			"tooltip":   fmt.Sprintf("package: %s", p.PkgPath),
			"rank":      "sink",
		}
		cluster.Clusters[name] = c
	}
	return cluster, nodes, edges
}
