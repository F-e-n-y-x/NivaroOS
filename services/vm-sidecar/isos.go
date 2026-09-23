// isos.go lists and accepts uploads of ISO files under the configured
// ISO directory, for the VM Manager app's Storage tab and the
// create-VM wizard's ISO picker.
package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	libvirt "libvirt.org/go/libvirt"
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

// isoNameRe is the whole allowed shape of an uploaded ISO's filename -
// a plain name (no directory separators) ending in .iso.
var isoNameRe = regexp.MustCompile(`(?i)^[A-Za-z0-9._ ()+-]+\.iso$`)

func validateISOFilename(name string) error {
	if strings.Contains(name, "..") || !isoNameRe.MatchString(name) {
		return badRequestf("invalid ISO filename %q: use letters, digits, spaces and . _ ( ) + - only, ending in .iso", name)
	}
	return nil
}

// uploadISO streams the multipart file field "iso" into isoDir/<filename>
// without ever buffering it in memory or /tmp (ISOs are gigabytes;
// r.FormFile spooled anything over 32 MiB to a temp file on the root
// filesystem first). The upload goes to a hidden temp file inside isoDir,
// and only a complete upload is moved into place - a dropped connection
// never leaves a truncated ISO in the library - and an existing ISO of the
// same name is never overwritten (409).
func uploadISO(isoDir string, r *http.Request) (ISOFile, error) {
	if err := os.MkdirAll(isoDir, 0755); err != nil {
		return ISOFile{}, fmt.Errorf("create ISO directory: %w", err)
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return ISOFile{}, badRequestf("read multipart upload: %v", err)
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return ISOFile{}, badRequestf(`missing multipart field "iso"`)
		}
		if err != nil {
			return ISOFile{}, badRequestf("read multipart upload: %v", err)
		}
		if part.FormName() != "iso" {
			part.Close()
			continue
		}
		defer part.Close()
		// The raw filename, not part.FileName() - that quietly applies
		// filepath.Base, turning "../../x.iso" into an acceptable "x.iso"
		// instead of letting it be rejected.
		name := part.FileName()
		if _, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition")); err == nil {
			name = params["filename"]
		}
		return saveISOPart(isoDir, name, part)
	}
}

func saveISOPart(isoDir, name string, src io.Reader) (ISOFile, error) {
	if err := validateISOFilename(name); err != nil {
		return ISOFile{}, err
	}
	destPath := filepath.Join(isoDir, name)
	if _, err := os.Lstat(destPath); err == nil {
		return ISOFile{}, conflictf("an ISO named %q already exists", name)
	}

	tmp, err := os.CreateTemp(isoDir, ".upload-*.part")
	if err != nil {
		return ISOFile{}, fmt.Errorf("create temp file in %s: %w", isoDir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once it's been linked/renamed into place

	written, err := io.Copy(tmp, src)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return ISOFile{}, fmt.Errorf("write %s: %w", destPath, err)
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		return ISOFile{}, err
	}

	// os.Link fails if destPath exists, making "don't overwrite" atomic even
	// against a concurrent upload of the same name; a filesystem without
	// hard links falls back to a (check-then-)rename.
	if err := os.Link(tmpPath, destPath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ISOFile{}, conflictf("an ISO named %q already exists", name)
		}
		if _, statErr := os.Lstat(destPath); statErr == nil {
			return ISOFile{}, conflictf("an ISO named %q already exists", name)
		}
		if err := os.Rename(tmpPath, destPath); err != nil {
			return ISOFile{}, fmt.Errorf("move upload into place: %w", err)
		}
	}
	return ISOFile{Name: name, SizeMiB: written / (1024 * 1024)}, nil
}

// deleteISO removes isoDir/<name> - filepath.Base strips any directory
// components from the client-supplied name first, the same protection
// uploadISO applies, so a crafted "../../etc/passwd" can't escape isoDir.
func deleteISO(isoDir, name string) error {
	name = filepath.Base(name)
	if !strings.EqualFold(filepath.Ext(name), ".iso") {
		return badRequestf("only .iso files can be removed here, got %q", name)
	}
	path := filepath.Join(isoDir, name)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return &statusError{status: http.StatusNotFound, msg: fmt.Sprintf("no such ISO %q", name)}
		}
		return err
	}
	return nil
}

// DomainsUsingPath lists the defined VMs with a disk or cdrom whose source
// is path (in their persistent config or, while running, live).
func (s *LibvirtStore) DomainsUsingPath(path string) ([]string, error) {
	conn, err := s.getConn()
	if err != nil {
		return nil, err
	}
	doms, err := conn.ListAllDomains(0)
	if err != nil {
		return nil, err
	}
	want := filepath.Clean(path)
	var users []string
	for i := range doms {
		dom := &doms[i]
		name, nameErr := dom.GetName()
		for _, flags := range []libvirt.DomainXMLFlags{libvirt.DOMAIN_XML_INACTIVE, 0} {
			xmlDesc, err := dom.GetXMLDesc(flags)
			if err != nil || nameErr != nil {
				continue
			}
			var parsed domainXML
			if err := xml.Unmarshal([]byte(xmlDesc), &parsed); err != nil {
				continue
			}
			found := false
			for _, d := range parsed.Devices.Disks {
				if d.Source.File != "" && filepath.Clean(d.Source.File) == want {
					found = true
				}
			}
			if found {
				users = append(users, name)
				break
			}
		}
		dom.Free()
	}
	return users, nil
}

func RegisterISORoutes(mux *http.ServeMux, store *LibvirtStore, isoDir string) {
	mux.HandleFunc("GET /isos", func(w http.ResponseWriter, r *http.Request) {
		isos, err := listISOs(isoDir)
		if err != nil {
			writeStoreError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, isos)
	})

	mux.HandleFunc("POST /isos", func(w http.ResponseWriter, r *http.Request) {
		iso, err := uploadISO(isoDir, r)
		if err != nil {
			writeStoreError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, iso)
	})

	mux.HandleFunc("DELETE /isos/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.PathValue("name"))
		// Deleting an ISO a VM still has inserted leaves that VM unable to
		// start ("Cannot access storage file") - eject it there first.
		users, err := store.DomainsUsingPath(filepath.Join(isoDir, name))
		if err != nil {
			writeStoreError(w, http.StatusInternalServerError, err)
			return
		}
		if len(users) > 0 {
			writeError(w, http.StatusConflict, fmt.Errorf("%q is still inserted in VM(s): %s - eject it there first", name, strings.Join(users, ", ")))
			return
		}
		if err := deleteISO(isoDir, name); err != nil {
			writeStoreError(w, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
