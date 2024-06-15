# tar

First I run `tar -cf out.tar tar.md` to add this file to the archive.

Extracting this file from `out.tar` you can see it just has the first line:

```
$ tar -Oxf out.tar tar.md
# tar
```

If instead of `-c` I use `tar -rf out.tar tar.md` to append `tar.md` to `tar.out`, you can see two entries:

```
$ tar -tvf out.tar
-rw-r--r--  0 jonjohnson staff       6 Jun 15 15:03 tar.md
-rw-r--r--  0 jonjohnson staff     154 Jun 15 15:05 tar.md
```

And if I try to extract `tar.md`, you'll see I get both entries.

````
$ tar -Oxf out.tar tar.md
# tar
# tar

First I run `tar -cf out.tar tar.md` to add this file to the archive.

Extracting this file from `out.tar`:

```
tar -Oxf out.tar tar.md
# tar
```
````
