package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"

	"github.com/skratchdot/open-golang/open"
	"golang.org/x/exp/maps"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	root, deps, sped, vers, err := gomod()
	if err != nil {
		return err
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr: l.Addr().String(),
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		roots := r.URL.Query()["n"]
		if len(roots) == 0 {
			roots = []string{root}
		}

		cmd := exec.Command("dot", "-Tsvg")
		cmd.Stdout = w

		in, err := cmd.StdinPipe()
		if err != nil {
			panic(err)
		}

		if err := cmd.Start(); err != nil {
			fmt.Fprintf(w, "error: %v", err)
		}

		todot(in, roots, deps, sped, vers)

		if err := in.Close(); err != nil {
			panic(err)
		}

		if err := cmd.Wait(); err != nil {
			fmt.Fprintf(w, "error: %v", err)
		}
	})

	if err := open.Run(fmt.Sprintf("http://localhost:%d", l.Addr().(*net.TCPAddr).Port)); err != nil {
		return err
	}

	return server.Serve(l)
}

func gomod() (string, map[string]map[string]struct{}, map[string]map[string]struct{}, map[string]string, error) {
	root := ""
	deps := map[string]map[string]struct{}{}
	sped := map[string]map[string]struct{}{}
	vers := map[string]string{}

	cmd := exec.Command("go", "mod", "graph")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil, nil, nil, err
	}

	if err := cmd.Start(); err != nil {
		return "", nil, nil, nil, err
	}

	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		line := scanner.Text()
		before, after, ok := strings.Cut(line, " ")
		if !ok {
			return "", nil, nil, nil, fmt.Errorf("weird line: %q", line)
		}

		if root == "" {
			root = before
		}

		if pkg, ver, ok := strings.Cut(before, "@"); ok {
			vers[pkg] = ver
		}
		if pkg, ver, ok := strings.Cut(after, "@"); ok {
			vers[pkg] = ver
		}

		if _, ok := deps[before]; !ok {
			deps[before] = map[string]struct{}{}
		}
		deps[before][after] = struct{}{}

		if _, ok := sped[after]; !ok {
			sped[after] = map[string]struct{}{}
		}
		sped[after][before] = struct{}{}
	}

	return root, deps, sped, vers, errors.Join(scanner.Err(), cmd.Wait())
}

func todot(w io.Writer, roots []string, deps, sped map[string]map[string]struct{}, vers map[string]string) {
	fmt.Fprintf(w, "digraph deps {\n")
	fmt.Fprintf(w, "\trankdir=LR;\n")

	u := url.URL{}
	q := u.Query()

	prev := ""

	for i, root := range roots {
		q.Add("n", root)
		u.RawQuery = q.Encode()

		if i+1 < len(roots) {
			fmt.Fprintf(w, "\t%q [href=%q, style=dashed];\n", root, u.String())

			if _, ok := deps[roots[i+1]][root]; ok {
				fmt.Fprintf(w, "\t%q -> %q [style=dashed, dir=back];\n", roots[i+1], root)
			} else {
				fmt.Fprintf(w, "\t%q -> %q [style=dashed];\n", root, roots[i+1])
			}

			prev = root

			continue
		}

		fmt.Fprintf(w, "\t%q [style=bold];\n", root)

		for dep := range deps[root] {
			href := fmt.Sprintf("%s&n=%s", u.String(), url.QueryEscape(dep))
			label := fmt.Sprintf("%s (%d)", dep, len(deps[dep]))
			fmt.Fprintf(w, "\t%q [href=%q, label=%q];\n", dep, href, label)
			fmt.Fprintf(w, "\t%q -> %q;\n", root, dep)
		}

		for dep := range sped[root] {
			// Skip this if it would just be a double edge.
			if dep == prev {
				continue
			}
			href := fmt.Sprintf("%s&n=%s", u.String(), url.QueryEscape(dep))
			label := fmt.Sprintf("%s (%d)", dep, len(deps[dep]))
			fmt.Fprintf(w, "\t%q [href=%q, label=%q];\n", dep, href, label)
			fmt.Fprintf(w, "\t%q -> %q;\n", dep, root)
		}
	}

	fmt.Fprintf(w, "versions [shape=record, label=\"{{pkg|{{")
	pkgs := maps.Keys(vers)
	for i, pkg := range pkgs {
		fmt.Fprintf(w, "%s", pkg)
		if i+1 < len(pkgs) {
			fmt.Fprintf(w, "|")
		}
	}
	fmt.Fprintf(w, "}}}|{ver|{{")
	for i, pkg := range pkgs {
		ver := vers[pkg]
		fmt.Fprintf(w, "%s", ver)
		if i+1 < len(pkgs) {
			fmt.Fprintf(w, "|")
		}
	}
	fmt.Fprintf(w, "}}}}")
	fmt.Fprintf(w, "\"];")

	fmt.Fprintf(w, "}\n")
}
