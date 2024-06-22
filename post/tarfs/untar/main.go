package main

import (
	"archive/tar"
	"io"
	"log"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	tmpdir, err := os.MkdirTemp("", "untar")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)

	if err := Untar(tmpdir, os.Stdin); err != nil {
		return err
	}

	for _, needle := range []string{"usr/lib/os-release", "var/lib/dpkg/triggers/lib32", "etc/hostname"} {
		f, err := os.Open(filepath.Join(tmpdir, needle))
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := io.Copy(os.Stdout, f); err != nil {
			return err
		}
	}

	return nil
}

func Untar(dst string, r io.Reader) error {
	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()

		switch {

		// if no more files are found return
		case err == io.EOF:
			return nil

		// return any other error
		case err != nil:
			return err

		// if the header is nil, just skip it (not sure how this happens)
		case header == nil:
			continue
		}

		// the target location where the dir/file should be created
		target := filepath.Join(dst, header.Name)

		// the following switch could also be done using fi.Mode(), not sure if there
		// a benefit of using one vs. the other.
		// fi := header.FileInfo()

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}

		// if it's a file create it
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			// copy over contents
			if _, err := io.Copy(f, tr); err != nil {
				return err
			}

			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			f.Close()
		}
	}
}
