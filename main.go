package main

import (
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	pkgname    string
	output     string
	nocompress bool

	N = 0

	files = map[string]File{}
	file  *os.File
)

type File struct {
	Func  string
	Size  int64
	CSize int64
	Time  time.Time
}

func main() {
	flag.StringVar(&pkgname, "pkgname", "main", "package name")
	flag.StringVar(&output, "o", "assets", "output file name")
	flag.BoolVar(&nocompress, "nc", false, "don't add compressed version")
	flag.Parse()

	createDataFile()
	createFuncFile()
}

func createDataFile() {
	name := output + "data.go"
	file, _ = os.Create(name)

	defer file.Close()

	fmt.Fprintln(file, "package "+pkgname)
	for _, v := range flag.Args() {
		filepath.Walk(v, walkpath)
	}
	exec.Command("gofmt", "-w", name).Output()
}

func createFuncFile() {
	name := output + ".go"
	file, _ = os.Create(name)

	defer file.Close()
	fmt.Fprintln(file, "package "+pkgname)
	fmt.Fprint(file, `

    import (
      "fmt"
      "mime"
      "net/http"
      "path"
      "strings"
      "time"
    )

    type assetFile struct {
      // contains plain data
      pdata func() []byte
      // contains compressed data
      cdata func() []byte
      // size of plain data
      psize uint64
      // size of compressed data
      csize uint64
      // File ModTime
      time  int64
    }

    func (e *assetFile) ModTime() time.Time {
      return time.Unix(e.time, 0)
    }

    // Size returns size of file data
    func (e *assetFile) Size(comp bool) string {
      if comp {
        return fmt.Sprint(e.csize)
      }
      return fmt.Sprint(e.psize)
    }

    // Comp returns true if file has compressed version
    func (e *assetFile) Comp() bool {
      return e.csize > 0
    }

    type assetFS map[string]*assetFile

    // Open returns asset file or error if not found
    func (fs assetFS) Open(name string) (*assetFile, error) {
      i, ok := fs[name]
      if !ok {
        return nil, fmt.Errorf("%s not found", name)
      }

      return i, nil
    }

    // Asset returns asset file plain data or error if not found
    func Asset(name string) ([]byte, error) {
      f, err := _bindata.Open(name)
      if err != nil {
        return nil, err
      }
      return f.pdata(), nil
    }

    // AssetZip returns asset file compressed data or error if not found
    func AssetZip(name string) ([]byte, error) {
      f, err := _bindata.Open(name)
      if err != nil {
        return nil, err
      }
      return f.cdata(), nil
    }

    // serveAssets returns handler function for assets in given directory
    func serveAssets(prefix string) func(w http.ResponseWriter, r *http.Request) {
      return func(w http.ResponseWriter, r *http.Request) {
        name := prefix + r.URL.Path

        f, err := _bindata.Open(name)

        if err != nil {
          http.NotFound(w, r)
          return
        }

        modtime := f.ModTime()

        // return if file not modified
        if t, err := time.Parse(http.TimeFormat, r.Header.Get("If-Modified-Since")); err == nil && modtime.Before(t.Add(1*time.Second)) {
          w.WriteHeader(http.StatusNotModified)
          return
        }

        mime := mime.TypeByExtension(path.Ext(name))

        w.Header().Set("Last-Modified", modtime.UTC().Format(http.TimeFormat))
        w.Header().Set("Content-Type", mime)

        if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && f.Comp() {
          // send compressed content if applicable
          w.Header().Set("Content-Encoding", "gzip")
          w.Header().Set("Content-Length", f.Size(true))
          w.WriteHeader(200)
          w.Write(f.cdata())
        } else {
          w.Header().Set("Content-Length", f.Size(false))
          w.WriteHeader(200)
          w.Write(f.pdata())
        }
      }
    }
  `)
	fmt.Fprintln(file, "var _bindata = assetFS{")

	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := files[k]
		fmt.Fprintf(file, "\"%s\": &assetFile{_%s, _c%s, %d, %d, %v},\n", k, v.Func, v.Func, v.Size, v.CSize, v.Time.Unix())
	}
	fmt.Fprintln(file, "}")
	exec.Command("gofmt", "-w", name).Output()
}

func walkpath(fpath string, f os.FileInfo, err error) error {
	if f.IsDir() {
		return nil
	}

	if strings.HasPrefix(f.Name(), ".") {
		return nil
	}

	fb, err := ioutil.ReadFile(fpath)

	if err != nil {
		return err
	}

	addFile(f, fpath, fb)

	return err
}

func addFile(f os.FileInfo, path string, fb []byte) (err error) {
	N++

	varN := fmt.Sprintf("bf%d", N)

	var csize int64
	var cb []byte

	if nocompress {
		cb = []byte{}
		csize = f.Size()
	} else {
		cb = compressed(fb)
		csize = int64(len(cb))
	}

	_, err = fmt.Fprintf(file, "var __%s = []byte(\"%s\")\n\n", varN, convert(fb))
	_, err = fmt.Fprintf(file, "func _%s() []byte {\n return __%s\n }\n\n", varN, varN)

	if f.Size() > csize {
		_, err = fmt.Fprintf(file, "var __c%s = []byte(\"%s\")\n\n", varN, convert(cb))
		_, err = fmt.Fprintf(file, "func _c%s() []byte {\n return __c%s\n }\n\n", varN, varN)
	} else {
		_, err = fmt.Fprintf(file, "func _c%s() []byte {\n return __%s\n }\n\n", varN, varN)
		csize = 0
	}

	files[path] = File{varN, f.Size(), csize, f.ModTime()}

	return
}

func compressed(src []byte) []byte {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	gz.Write(src)
	gz.Close()
	return b.Bytes()
}

func convert(b []byte) string {
	s := fmt.Sprintf("%#x", b)
	return strings.Replace(s, "0x", "\\x", -1)
}
