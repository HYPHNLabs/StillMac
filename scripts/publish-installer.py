#!/usr/bin/env python3
"""Publish an activated installer without pathname TOCTOU or replacement."""
import os
import stat
import sys


def sync_directory(fd):
    os.fsync(fd)


def fail(message):
    print(message, file=sys.stderr)
    return 2


def main(argv):
    if len(argv) != 4:
        return fail("usage: publish-installer.py TEMPLATE PARENT BASENAME DIGEST")
    template, parent, name, digest = argv[0:4]
    if not name or name in (".", "..") or "/" in name or os.sep in name or (os.altsep and os.altsep in name):
        return fail("output basename is unsafe")
    if len(digest) != 64 or any(c not in "0123456789abcdefABCDEF" for c in digest):
        return fail("manifest digest is invalid")

    try:
        with open(template, "rb") as source:
            data = source.read()
    except OSError as exc:
        return fail(f"cannot read installer template: {exc}")
    placeholder = b"@TRUSTED_MANIFEST_SHA256@"
    if data.count(placeholder) != 1:
        return fail("installer template must contain exactly one trust placeholder")
    data = data.replace(placeholder, digest.encode("ascii"))

    flags = os.O_RDONLY | getattr(os, "O_DIRECTORY", 0) | getattr(os, "O_NOFOLLOW", 0)
    try:
        parent_fd = os.open(parent, flags)
    except OSError as exc:
        return fail(f"output parent is unsafe: {exc}")
    created = False
    output_fd = None
    try:
        st = os.fstat(parent_fd)
        if not stat.S_ISDIR(st.st_mode) or st.st_uid != os.getuid() or st.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
            return fail("output parent is unsafe")
        output_fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0), 0o755, dir_fd=parent_fd)
        created = True
        view = memoryview(data)
        while view:
            view = view[os.write(output_fd, view):]
        os.fsync(output_fd)
        os.close(output_fd)
        output_fd = None
        sync_directory(parent_fd)
        created = False
    except OSError as exc:
        return fail(f"exclusive installer publication failed: {exc}")
    finally:
        if output_fd is not None:
            try:
                os.close(output_fd)
            except OSError:
                pass
        if created:
            try:
                os.unlink(name, dir_fd=parent_fd)
            except OSError:
                pass
        os.close(parent_fd)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
