package main

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"time"

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
	fmt.Println("tarfs")

	timings := make([]time.Duration, 0, 3000)
	start := time.Now()

	stat, err := os.Stdin.Stat()
	if err != nil {
		return err
	}

	fsys, err := tarfs.New(os.Stdin, stat.Size())
	if err != nil {
		return err
	}

	if err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if d.Type().IsRegular() {
			f, err := fsys.Open(name)
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err := io.Copy(io.Discard, f); err != nil {
				return err
			}
			timings = append(timings, time.Since(start))
		}

		return nil
	}); err != nil {
		return err
	}

	for _, t := range timings {
		fmt.Println(t.Microseconds())
	}

	return nil
}
