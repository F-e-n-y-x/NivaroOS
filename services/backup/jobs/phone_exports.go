package jobs

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Export categories keep one file per run (phone_model.go). sms and
// calllog use the SMS Backup & Restore XML format and are incremental:
// the phone asks which item keys are new (check-items) and sends only
// those; the server records the keys of every export it stores.
//
// Item keys (lowercase hex SHA-256 of the UTF-8 string):
//
//	sms   sha256("sms|" + address + "|" + date + "|" + type + "|" + body)
//	mms   sha256("mms|" + address + "|" + date + "|" + msg_box + "|" + m_id)
//	call  sha256("call|" + number + "|" + date + "|" + duration)
//
// with the attribute values exactly as in the XML (date in unix ms).

func NewExportID() string { return "exp_" + randomHex(8) }

// cleanExportName checks an export stream name: calendar needs one
// (1..128 characters, no control characters or slashes), the others
// take none.
func cleanExportName(cat PhoneCategory, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if !cat.info().named {
		return "", name == ""
	}
	if name == "" || utf8.RuneCountInString(name) > 128 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\\x00") {
		return "", false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return name, true
}

func (r PhoneExportRow) toAPI() PhoneExport {
	return PhoneExport{
		ID: r.ID, Category: PhoneCategory(r.Category), Name: r.Name, TakenAt: r.TakenAt, Size: r.Size,
		SHA256: r.SHA256, Items: r.Items, Encrypted: r.Encrypted, Imported: r.Imported,
	}
}

// itemKey is the item key of one SMS Backup & Restore element (sms, mms
// or call), "" for anything else.
func itemKey(el xml.StartElement) string {
	a := map[string]string{}
	for _, at := range el.Attr {
		a[at.Name.Local] = at.Value
	}
	var s string
	switch el.Name.Local {
	case "sms":
		s = "sms|" + a["address"] + "|" + a["date"] + "|" + a["type"] + "|" + a["body"]
	case "mms":
		s = "mms|" + a["address"] + "|" + a["date"] + "|" + a["msg_box"] + "|" + a["m_id"]
	case "call":
		s = "call|" + a["number"] + "|" + a["date"] + "|" + a["duration"]
	default:
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ItemKey computes the item key the phone must send (exported for the
// contract doc tests): kind is sms, mms or call; fields in the order
// above.
func ItemKey(kind string, fields ...string) string {
	sum := sha256.Sum256([]byte(kind + "|" + strings.Join(fields, "|")))
	return hex.EncodeToString(sum[:])
}

// scanItems reads the item keys of an SMS Backup & Restore file, in
// order (duplicates kept).
func scanItems(r io.Reader) ([]string, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	depth := 0
	var keys []string
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return keys, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				if k := itemKey(t); k != "" {
					keys = append(keys, k)
				}
			}
		case xml.EndElement:
			depth--
		}
	}
}

// countItems counts the entries of a non-incremental export.
func countItems(cat PhoneCategory, p string) int64 {
	f, err := os.Open(p)
	if err != nil {
		return 0
	}
	defer f.Close()
	switch cat {
	case CatContacts, CatCalendar:
		marker := "BEGIN:VCARD"
		if cat == CatCalendar {
			marker = "BEGIN:VEVENT"
		}
		var n int64
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 16<<20)
		for sc.Scan() {
			if strings.EqualFold(strings.TrimSpace(sc.Text()), marker) {
				n++
			}
		}
		return n
	case CatApps:
		var v interface{}
		if json.NewDecoder(io.LimitReader(f, 64<<20)).Decode(&v) != nil {
			return 0
		}
		switch t := v.(type) {
		case []interface{}:
			return int64(len(t))
		case map[string]interface{}:
			if list, ok := t["apps"].([]interface{}); ok {
				return int64(len(list))
			}
		}
	}
	return 0
}

// exportFileName is a free name for a new export of cat.
func exportRel(root string, cat PhoneCategory, name string, now time.Time) string {
	info := cat.info()
	dir := info.dir
	if info.named {
		stem := deviceFolderStem(name)
		if stem == "" {
			stem = "calendar"
		}
		dir = path.Join(dir, stem)
	}
	ts := now.UTC().Format(phoneTimeLayout)
	rel := path.Join(dir, info.prefix+"-"+ts+info.ext)
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); errors.Is(err, os.ErrNotExist) {
			return rel
		}
		rel = path.Join(dir, fmt.Sprintf("%s-%s-%d%s", info.prefix, ts, i, info.ext))
	}
}

// placeExport stores a complete export (device lock held). It answers
// stored, unchanged or no_new_items.
func (s *Service) placeExport(root string, d DeviceRow, sessionID string, cat PhoneCategory, up PhoneUploadRow, part string, imported bool) (string, error) {
	info := cat.info()
	var newest PhoneExportRow
	hasNewest := s.store.db.Where("device_id = ? AND category = ? AND name = ?", d.ID, string(cat), up.Name).
		Order("taken_at DESC").Take(&newest).Error == nil
	if hasNewest && newest.SHA256 == up.SHA256 && !info.incremental {
		_ = os.Remove(part)
		return UploadUnchanged, nil
	}
	var newKeys []string
	items := int64(0)
	if info.incremental && !up.Encrypted {
		f, err := os.Open(part)
		if err != nil {
			return "", engine.Errorf(ErrIOError, "%s: %v", part, err)
		}
		keys, err := scanItems(f)
		f.Close()
		if err != nil {
			_ = os.Remove(part)
			return "", engine.Errorf(ErrValidation, "the %s export is not SMS Backup & Restore XML: %v", cat, err)
		}
		seen := map[string]bool{}
		var uniq []string
		for _, k := range keys {
			if !seen[k] {
				seen[k] = true
				uniq = append(uniq, k)
			}
		}
		have := map[string]bool{}
		for _, chunk := range chunks(uniq, 400) {
			var rows []PhoneItemRow
			if err := s.store.db.Where("device_id = ? AND category = ? AND key IN ?", d.ID, string(cat), chunk).Find(&rows).Error; err != nil {
				return "", err
			}
			for _, r := range rows {
				have[r.Key] = true
			}
		}
		for _, k := range uniq {
			if !have[k] {
				newKeys = append(newKeys, k)
			}
		}
		if len(newKeys) == 0 {
			_ = os.Remove(part)
			return UploadNoNewItems, nil
		}
		items = int64(len(keys))
	} else if !up.Encrypted {
		items = countItems(cat, part)
	}
	now := s.phoneNow()
	rel := exportRel(root, cat, up.Name, now)
	dir, err := safeDir(root, path.Dir(rel))
	if err != nil {
		return "", err
	}
	if err := os.Rename(part, filepath.Join(dir, path.Base(rel))); err != nil {
		return "", engine.Errorf(ErrIOError, "placing the %s export: %v", cat, err)
	}
	row := PhoneExportRow{
		ID: NewExportID(), DeviceID: d.ID, Category: string(cat), Name: up.Name, SessionID: sessionID, TakenAt: now,
		Stored: rel, Size: up.Length, SHA256: up.SHA256, Items: items, Encrypted: up.Encrypted, Imported: imported,
	}
	err = s.store.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		for _, chunk := range chunks(newKeys, 300) {
			rows := make([]PhoneItemRow, len(chunk))
			for i, k := range chunk {
				rows[i] = PhoneItemRow{DeviceID: d.ID, Category: string(cat), Key: k}
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("store: record export: %w", err)
	}
	return UploadStored, nil
}

// exportsAt are the exports in effect at snapshot time t (nil: now):
// the newest of each full category and name, every incremental one.
func (s *Service) exportsAt(deviceID string, cat PhoneCategory, t *time.Time) ([]PhoneExportRow, error) {
	q := s.store.db.Where("device_id = ?", deviceID)
	if cat != "" {
		q = q.Where("category = ?", string(cat))
	}
	if t != nil {
		q = q.Where("taken_at <= ?", *t)
	}
	var rows []PhoneExportRow
	if err := q.Order("category, name, taken_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := []PhoneExportRow{}
	seen := map[string]bool{}
	for _, r := range rows {
		c := PhoneCategory(r.Category)
		if c.info().incremental {
			out = append(out, r)
			continue
		}
		k := r.Category + "\x00" + r.Name
		if !seen[k] {
			seen[k] = true
			out = append(out, r)
		}
	}
	return out, nil
}

// writeFullXML writes one SMS Backup & Restore file with every item of
// the given exports (oldest first), each item once.
func writeFullXML(w io.Writer, root string, cat PhoneCategory, rows []PhoneExportRow) error {
	outer := "smses"
	if cat == CatCallLog {
		outer = "calls"
	}
	// Pass 1: count the distinct items.
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Encrypted {
			continue
		}
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(r.Stored)))
		if err != nil {
			return err
		}
		keys, err := scanItems(f)
		f.Close()
		if err != nil {
			return err
		}
		for _, k := range keys {
			seen[k] = true
		}
	}
	total := len(seen)
	bw := bufio.NewWriterSize(w, 256<<10)
	fmt.Fprintf(bw, "<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>\n<%s count=\"%d\" backup_date=\"%d\" type=\"full\">\n", outer, total, time.Now().UnixMilli())
	written := map[string]bool{}
	for _, r := range rows {
		if r.Encrypted {
			continue
		}
		if err := copyItems(bw, filepath.Join(root, filepath.FromSlash(r.Stored)), written); err != nil {
			return err
		}
	}
	fmt.Fprintf(bw, "</%s>\n", outer)
	return bw.Flush()
}

// copyItems copies the depth-2 elements of one file whose key isn't in
// written yet.
func copyItems(w io.Writer, p string, written map[string]bool) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := xml.NewDecoder(bufio.NewReader(f))
	dec.Strict = false
	depth := 0
	var enc *xml.Encoder
	var buf bytes.Buffer
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		tok = xml.CopyToken(tok)
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				k := itemKey(t)
				if k != "" && !written[k] {
					written[k] = true
					buf.Reset()
					enc = xml.NewEncoder(&buf)
				} else {
					enc = nil
				}
			}
			if depth >= 2 && enc != nil {
				t.Name.Space = ""
				for i := range t.Attr {
					t.Attr[i].Name.Space = ""
				}
				if err := enc.EncodeToken(t); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if depth >= 2 && enc != nil {
				t.Name.Space = ""
				if err := enc.EncodeToken(t); err != nil {
					return err
				}
				if depth == 2 {
					if err := enc.Flush(); err != nil {
						return err
					}
					buf.WriteString("\n")
					if _, err := w.Write(buf.Bytes()); err != nil {
						return err
					}
					enc = nil
				}
			}
			depth--
		case xml.CharData:
			if depth >= 2 && enc != nil {
				if err := enc.EncodeToken(t); err != nil {
					return err
				}
			}
		}
	}
}

// handleExportImport is POST /devices/:id/exports/import?category=: an
// SMS Backup & Restore file made elsewhere, stored as an export (new
// items only count).
func (s *Service) handleExportImport(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	d, err := s.store.GetDevice(r.PathValue("id"))
	if err != nil {
		s.pfail(w, err)
		return
	}
	cat := PhoneCategory(r.URL.Query().Get("category"))
	if !cat.valid() || !cat.info().incremental {
		writeValidation(w, map[string]string{"category": string(FieldInvalid)})
		return
	}
	if r.ContentLength > PhoneMaxImportSize {
		writeError(w, http.StatusRequestEntityTooLarge, ErrorBody{ErrorCode: ErrTooLarge, Detail: "at most 1 GiB"})
		return
	}
	lock := s.phone.devLock(d.ID)
	lock.Lock()
	defer lock.Unlock()
	root, err := s.writableRoot(r.Context(), &d)
	if err != nil {
		s.pfail(w, err)
		return
	}
	if r.ContentLength > 0 {
		if err := needSpace(root, r.ContentLength); err != nil {
			s.pfail(w, err)
			return
		}
	}
	part, err := s.uploadTemp(root, "import_"+randomHex(8)+".part")
	if err != nil {
		s.pfail(w, err)
		return
	}
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, phoneFileMode)
	if err != nil {
		s.pfail(w, engine.Errorf(ErrIOError, "%s: %v", part, err))
		return
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r.Body, PhoneMaxImportSize+1))
	f.Close()
	if err != nil || n > PhoneMaxImportSize || n == 0 {
		_ = os.Remove(part)
		if n > PhoneMaxImportSize {
			writeError(w, http.StatusRequestEntityTooLarge, ErrorBody{ErrorCode: ErrTooLarge, Detail: "at most 1 GiB"})
			return
		}
		writeValidation(w, map[string]string{"body": string(FieldRequired)})
		return
	}
	up := PhoneUploadRow{Category: string(cat), SHA256: hex.EncodeToString(h.Sum(nil)), Length: n}
	result, err := s.placeExport(root, d, "", cat, up, part, true)
	if err != nil {
		_ = os.Remove(part)
		s.pfail(w, err)
		return
	}
	s.audit(r, "device_import", "", fmt.Sprintf("device=%s category=%s bytes=%d result=%s", d.ID, cat, n, result))
	s.deviceChanged(d.ID, DeviceChangeImported)
	var row PhoneExportRow
	out := ImportResult{Result: result}
	if result == UploadStored && s.store.db.Where("device_id = ? AND category = ?", d.ID, string(cat)).Order("taken_at DESC").Take(&row).Error == nil {
		e := row.toAPI()
		out.Export = &e
	}
	writeOK(w, http.StatusOK, out)
}

// ImportResult answers POST /devices/:id/exports/import.
type ImportResult struct {
	// Result is stored or no_new_items.
	Result string       `json:"result"`
	Export *PhoneExport `json:"export"`
}

func parseBoolQ(v string) bool { b, _ := strconv.ParseBool(v); return b || v == "1" }
