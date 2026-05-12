**grrep** <sup>_/[ɡɜːr]ep/_ Global Rapid Regular Expression Print</sup><br/><br/>
A small and [fast](#benchmark) recursive grep written in Go. Install with `go github.com/bep/grrep@latest`.

```
usage: grrep [-q] [-F] [-i] [-w] [-v] [-d N] [--hidden] [--no-ignore] PATTERN [PATH]

Flags:
  -F                 treat PATTERN as a fixed string, not a regex
  -i                 case-insensitive match
  -d, --max-depth=N  search at most N directory levels (1 = root only, 0 = nothing)
  --hidden           search hidden files and directories (.git is always skipped)
  --no-ignore        do not respect .gitignore/.ignore files
  -q                 quiet: suppress match output
  -v                 select non-matching lines
  -w                 match only at word boundaries
```

## Why

I needed a search tool that plays nicely with [gitjoin](https://github.com/bep/gitjoin) — the joined-in subrepositories are listed as `.gitignore` entries in the host repo, and a normal grep would refuse to descend into them. grrep skips the gitjoin-managed block when reading `.gitignore`, so the joined repos remain searchable as one tree.

The other motivation was curiosity: how far Go and the standard library can take a tool like this before reaching for non-stdlib regex/SIMD or unsafe code.

## Behavior

* Honors `.gitignore` and `.ignore` files in the search tree.
* Does **not** honor `~/.gitignore_global`.
* Skips the `# Managed by gitjoin … # End gitjoin managed section` block when reading any `.gitignore`.
* Skips hidden files and directories by default; pass `--hidden` to include them (`.git` is always skipped).
* Skips files whose first 8 KiB contain a NUL byte (binary heuristic).

## Benchmark

Similar shape to the first table in [ripgrep's "Quick examples comparing tools"](https://github.com/burntsushi/ripgrep#quick-examples-comparing-tools), on a MacBook Pro M1 (32 GB) against [the current torvalds/linux](https://github.com/torvalds/linux) after `make defconfig && make -j8`.

Tree:

| metric | count |
|---|---|
| files (excluding hidden) | 118,270 |
| files visible after `.gitignore` | 93,593 |
| files filtered by `.gitignore` | 24,677 |

Pattern: `[A-Z]+_SUSPEND` matched as a whole word (`-w`). All three tools find the same 575 matches.

| tool | median wall (n=7) |
|---|---|
| ugrep   | 1.184s |
| grrep   | 1.664s |
| ripgrep | 3.467s |

Reproduce with `bash bench.sh`.

## Setting up the benchmark tree on macOS

The Linux source contains paths that collide on a case-insensitive filesystem (e.g. `Documentation/Kbuild` and `Documentation/kbuild/`). Clone into a case-sensitive volume, then build inside a Linux container:

```sh
diskutil list                                                # find the APFS container (often disk3)
diskutil apfs addVolume disk3 'Case-sensitive APFS' LinuxBench
cd /Volumes/LinuxBench
git clone --depth=1 https://github.com/torvalds/linux.git

docker run --rm \
  -v /Volumes/LinuxBench/linux:/src -w /src \
  ubuntu:24.04 \
  bash -c '
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -yqq --no-install-recommends \
      build-essential bc bison flex libssl-dev libelf-dev cpio kmod \
      python3 rsync dwarves
    make defconfig && make -j8
  '
```

The build creates the ~25 K `.o` / `.cmd` / etc. artifacts that make the `.gitignore` path matter.
