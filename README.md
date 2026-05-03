

## Benchmark info

Note that my only intention with these benchmarks is to get a rough idea of how this tool performs compared to some others, and maybe show that you can get decent performance out of a relatively simple Go implementation. Note that the benchmarks below are run on a MacBook Pro M1 with 32 GB of RAM. I have read somewhere that `ripgrep` is highly optimized on x64 architectures, which may help explain that number.

On Macos:

```
diskutil list                                                 # find your APFS container (likely disk3)
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