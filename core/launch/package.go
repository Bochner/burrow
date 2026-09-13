package launch

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Hovel v0.4.2, source c461ba282a8aecc7aa3a079a4613bf5e2640c388.
// Wheel pin matches MODULE.bazel; executable digest is from its exact member.
const WheelSHA = "7edccdabe04e30098e417d27ab48064c11a8612149124b7df0797251e88a0933"
const ExecutableSHA = "a7bcd5f2fa885244d160b722a6ffbd28f3bbae9777b9d510119a203d08788cc1"
const PackageURL = "https://github.com/vibepwners/hovel/releases/download/v0.4.2/hovel-0.4.2-py3-none-manylinux_2_28_x86_64.whl"
const packageLimit = 128 << 20

type Options struct {
	Workspace string
	Package   string // Optional local copy of the exact pinned wheel, never a pin override.
	Offline   bool
}

func install(ctx context.Context, o Options) (string, error) {
	defer Phase("install")()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return "", fmt.Errorf("initial package supports Linux amd64 only")
	}
	cache, e := os.UserCacheDir()
	if e != nil {
		return "", e
	}
	path := filepath.Join(cache, "burrow", "hovel", "0.4.2")
	dir, e := directory(path, true, true)
	if e != nil {
		return "", e
	}
	defer dir.Close()
	if e = lock(ctx, dir); e != nil {
		return "", e
	}
	for _, name := range []string{"hovel.pending", "hovel.whl.pending"} {
		if e = absent(filepath.Join(path, name)); e != nil {
			return "", e
		}
	}
	executable := filepath.Join(path, "hovel")
	installed, e := regularDigest(executable, 0700, packageLimit)
	if e == nil {
		if installed != ExecutableSHA {
			return "", refuse(executable, "executable checksum differs from pinned package")
		}
		return executable, nil
	}
	if !os.IsNotExist(e) {
		return "", e
	}
	wheel := filepath.Join(path, "hovel.whl")
	data, e := regular(wheel, 0600, packageLimit)
	if os.IsNotExist(e) {
		var source io.ReadCloser
		if o.Package != "" {
			source, e = os.Open(o.Package)
		} else if o.Offline {
			return "", fmt.Errorf("pinned Hovel package is not cached; supply --hovel-package FILE or retry online")
		} else {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, PackageURL, nil)
			if err != nil {
				return "", err
			}
			client := http.Client{Timeout: 45 * time.Second}
			response, err := client.Do(request)
			if err != nil {
				return "", fmt.Errorf("Hovel download unavailable: %w; retry or supply --hovel-package FILE", err)
			}
			if response.StatusCode != 200 {
				response.Body.Close()
				return "", fmt.Errorf("Hovel download returned HTTP %d; retry or supply --hovel-package FILE", response.StatusCode)
			}
			source, e = response.Body, nil
		}
		if e != nil {
			return "", fmt.Errorf("Hovel package unavailable: %w", e)
		}
		data, e = io.ReadAll(io.LimitReader(source, packageLimit+1))
		source.Close()
		if e != nil {
			return "", e
		}
		if len(data) > packageLimit || sum(data) != WheelSHA {
			return "", fmt.Errorf("Hovel package verification failed; obtain the pinned v0.4.2 Linux amd64 wheel; nothing installed")
		}
		if e = publish(dir, "hovel.whl", data, 0600); e != nil {
			return "", e
		}
	} else if e != nil {
		return "", e
	}
	if sum(data) != WheelSHA {
		return "", refuse(wheel, "package checksum mismatch")
	}
	archive, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		return "", e
	}
	var binary []byte
	for _, entry := range archive.File {
		if entry.Name != "hovel/bin/hovel" {
			continue
		}
		if binary != nil {
			return "", fmt.Errorf("duplicate executable in pinned package")
		}
		r, err := entry.Open()
		if err != nil {
			return "", err
		}
		binary, err = io.ReadAll(io.LimitReader(r, packageLimit+1))
		r.Close()
		if err != nil {
			return "", err
		}
	}
	if sum(binary) != ExecutableSHA {
		return "", fmt.Errorf("Hovel executable verification failed")
	}
	if e = publish(dir, "hovel", binary, 0700); e != nil {
		return "", e
	}
	return executable, nil
}
