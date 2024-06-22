package main

import (
	"io"
	"log"
	"os"

	"github.com/jonjohnsonjr/targz/tarfs"
)

var nearTheEnd = []string{
	"var/lib/systemd/deb-systemd-helper-enabled/apt-daily-upgrade.timer.dsh-also",
	"var/lib/systemd/deb-systemd-helper-enabled/apt-daily.timer.dsh-also",
	"var/lib/systemd/deb-systemd-helper-enabled/dpkg-db-backup.timer.dsh-also",
	"var/lib/systemd/deb-systemd-helper-enabled/e2scrub_all.timer.dsh-also",
	"var/lib/systemd/deb-systemd-helper-enabled/e2scrub_reap.service.dsh-also",
	"var/lib/systemd/deb-systemd-helper-enabled/fstrim.timer.dsh-also",
	"var/lib/systemd/deb-systemd-helper-enabled/motd-news.timer.dsh-also",
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return err
	}

	fsys, err := tarfs.New(os.Stdin, stat.Size())
	if err != nil {
		return err
	}

	// for _, needle := range []string{"usr/lib/os-release", "var/lib/dpkg/triggers/lib32", "etc/hostname"} {
	for _, needle := range nearTheEnd {
		f, err := fsys.Open(needle)
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
