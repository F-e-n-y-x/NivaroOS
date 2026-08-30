// isos.go lists and accepts uploads of ISO files under the configured
// ISO directory, for the VM Manager app's Storage tab and the
// create-VM wizard's ISO picker.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type ISOFile struct {
	Name    string `json:"name"`
	SizeMiB int64  `json:"size_mib"`
}

func listISOs(isoDir string) ([]ISOFile, error) {
	entries, err := os.ReadDir(isoDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ISOFile{}, nil
		}
		return nil, err
	}
	isos := make([]ISOFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".iso") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		isos = append(isos, ISOFile{Name: e.Name(), SizeMiB: info.Size() / (1024 * 1024)})
	}
	return isos, nil
}

// uploadISO streams the multipart file field "iso" into isoDir/<filename>.
// filepath.Base strips any directory components from the client-supplied
// filename, so a crafted "../../etc/evil.iso" still lands inside isoDir
// as plain "evil.iso" rather than escaping it.
func uploadISO(isoDir string, r *http.Request) (ISOFile, error) {
	if err := os.MkdirAll(isoDir, 0755); err != nil {
		return ISOFile{}, fmt.Errorf("create ISO directory: %w", err)
	}
	file, header, err := r.FormFile("iso")
	if err != nil {
		return ISOFile{}, fmt.Errorf(`read multipart field "iso": %w`, err)
	}
	defer file.Close()

	name := filepath.Base(header.Filename)
	if !strings.EqualFold(filepath.Ext(name), ".iso") {
		return ISOFile{}, fmt.Errorf("only .iso files are accepted, got %q", name)
	}

	destPath := filepath.Join(isoDir, name)
	dest, err := os.Create(destPath)
	if err != nil {
		return ISOFile{}, fmt.Errorf("create %s: %w", destPath, err)
	}
	defer dest.Close()

	written, err := io.Copy(dest, file)
	if err != nil {
		return ISOFile{}, fmt.Errorf("write %s: %w", destPath, err)
	}
	return ISOFile{Name: name, SizeMiB: written / (1024 * 1024)}, nil
}

// deleteISO removes isoDir/<name> - filepath.Base strips any directory
// components from the client-supplied name first, the same protection
// uploadISO applies, so a crafted "../../etc/passwd" can't escape isoDir.
func deleteISO(isoDir, name string) error {
	name = filepath.Base(name)
	if !strings.EqualFold(filepath.Ext(name), ".iso") {
		return fmt.Errorf("only .iso files can be removed here, got %q", name)
	}
	path := filepath.Join(isoDir, name)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no such ISO %q", name)
		}
		return err
	}
	return nil
}

func RegisterISORoutes(mux *http.ServeMux, isoDir string) {
	mux.HandleFunc("GET /isos", func(w http.ResponseWriter, r *http.Request) {
		isos, err := listISOs(isoDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, isos)
	})

	mux.HandleFunc("POST /isos", func(w http.ResponseWriter, r *http.Request) {
		iso, err := uploadISO(isoDir, r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, iso)
	})

	mux.HandleFunc("DELETE /isos/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := deleteISO(isoDir, r.PathValue("name")); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
