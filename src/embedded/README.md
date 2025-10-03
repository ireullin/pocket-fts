# Embedded Library

This directory contains `libftscore.so` which is embedded into the `pocket_fts` binary at compile time.

## How it works

1. **Compile time**: The library is embedded using Go's `//go:embed` directive
2. **Runtime**: When the program starts, it extracts the library to the same directory as `db.sqlite`
3. **Dynamic loading**: The library is then loaded from that location using `dlopen`

## Benefits

- Single binary distribution (no need to ship `.so` separately)
- Library is automatically placed next to the database file
- No need to set `LD_LIBRARY_PATH` or install system-wide

## File location at runtime

If your database is at `/path/to/db.sqlite`, the library will be extracted to `/path/to/libftscore.so`.
