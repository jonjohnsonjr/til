package main

import (
	"archive/tar"
	"fmt"
	"io"
	"log"
	"os"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

var nearTheEnd = []string{
	"var/lib/systemd/deb-systemd-helper-enabled/apt-daily-upgrade.timer.dsh-also",
	"var/lib/systemd/deb-systemd-helper-enabled/apt-daily.timer.dsh-also",
	"var/lib/systemd/deb-systemd-helper-enabled/dpkg-db-backup.timer.dsh-also",
	"var/lib/systemd/deb-systemd-helper-enabled/e2scrub_all.timer.dsh-also",
	"var/lib/systemd/deb-systemd-helper-enabled/e2scrub_reap.service.dsh-also",
	"var/lib/systemd/deb-systemd-helper-enabled/fstrim.timer.dsh-also",
	"var/lib/systemd/deb-systemd-helper-enabled/motd-news.timer.dsh-also",
}

func run() error {
	// for _, needle := range []string{"usr/lib/os-release", "var/lib/dpkg/triggers/lib32", "etc/hostname"} {
	for _, needle := range nearTheEnd {
		f, err := Scan(needle, os.Stdin)
		if err != nil {
			return err
		}

		if _, err := io.Copy(os.Stdout, f); err != nil {
			return err
		}

		if _, err := os.Stdin.Seek(io.SeekStart, 0); err != nil {
			return err
		}
	}

	return nil
}

func Scan(needle string, r io.Reader) (io.Reader, error) {
	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()

		// if no more files are found return
		if err == io.EOF {
			return nil, fmt.Errorf("did not see %q in tar", needle)
		}

		// return any other error
		if err != nil {
			return nil, err
		}

		if header.Name != needle {
			continue
		}

		return tr, nil
	}
}
