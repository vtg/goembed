package main

import (
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var pkg string
var output string
var gzipOff bool

type File struct {
	Func string
	Size int64
	Time time.Time
}

var N = 0

var files = map[string]File{}
var file *os.File

func main() {
	flag.StringVar(&pkg, "pkg", "main", "package name")
	flag.StringVar(&output, "o", "assets", "output file name")
	flag.BoolVar(&gzipOff, "nz", false, "don't add gzip version")
	flag.Parse()

	createDataFile()
	createFuncFile()
}

func createDataFile() {
	name := output + "data.go"
	file, _ = os.Create(name)

	defer file.Close()

	fmt.Fprintln(file, "package main")
	for _, v := range flag.Args() {
		filepath.Walk(v, walkpath)
	}
	exec.Command("gofmt", "-w", name).Output()
}

func createFuncFile() {
	name := output + ".go"
	file, _ = os.Create(name)

	defer file.Close()

	fmt.Fprint(file, `
    package main

    import (
      "fmt"
      "mime"
      "net/http"
      "path"
      "strings"
      "time"
    )

    type assetFile struct {
      data func() []byte
      size string
      time int64
    }

    func (e *assetFile) Read() []byte {
      return e.data()
    }

    func (e *assetFile) ModTime() time.Time {
      return time.Unix(e.time, 0)
    }

    func (e *assetFile) Size() string {
      return e.size
    }

    type assetFS map[string]*assetFile

    func (fs assetFS) ZipName(name string) (string, bool) {
      zipName := name + ".gz"
      if _, ok := fs[zipName]; ok {
        return zipName, ok
      }
      return name, false
    }

    func (fs assetFS) Open(name string) (*assetFile, error) {
      i, ok := fs[name]
      if !ok {
        return nil, fmt.Errorf("%s not found", name)
      }

      return i, nil
    }

    func Asset(name string) ([]byte, error) {
      f, err := _bindata.Open(name)
      if err != nil {
        return nil, err
      }
      return f.Read(), nil
    }

    func AssetZip(name string) ([]byte, error) {
      return Asset(name + ".gz")
    }

    func serveAssets(prefix string) func(w http.ResponseWriter, r *http.Request) {
      return func(w http.ResponseWriter, r *http.Request) {
        var f *assetFile
        var err error

        name := prefix + r.URL.Path
        ext := path.Ext(name)

        // send encoded content if applicable
        sendZip := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")

        if sendZip {
          name, sendZip = _bindata.ZipName(name)
        }

        if f, err = _bindata.Open(name); err != nil {
          http.NotFound(w, r)
          return
        }

        modtime := f.ModTime()

        // return if file not modified
        if t, err := time.Parse(http.TimeFormat, r.Header.Get("If-Modified-Since")); err == nil && modtime.Before(t.Add(1*time.Second)) {
          w.WriteHeader(http.StatusNotModified)
          return
        }

        mime := mime.TypeByExtension(ext)

        if sendZip {
          w.Header().Set("Content-Encoding", "gzip")
        }

        w.Header().Set("Last-Modified", modtime.UTC().Format(http.TimeFormat))
        w.Header().Set("Content-Type", mime)
        w.Header().Set("Content-Length", f.Size())

        w.WriteHeader(200)
        w.Write(f.Read())
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
		fmt.Fprintf(file, "\"%s\": &assetFile{%s, \"%d\", %v},\n", k, v.Func, v.Size, v.Time.Unix())
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

	addFile(f, fpath, fb, false)

	if !gzipOff && path.Ext(fpath) != ".gz" {
		addFile(f, fpath+".gz", fb, true)
	}

	return err
}

func addFile(f os.FileInfo, path string, fb []byte, zip bool) (err error) {
	var b bytes.Buffer
	var l int64
	N++
	varN := fmt.Sprintf("bf%d", N)

	if zip {
		w := gzip.NewWriter(&b)
		w.Write(fb)
		w.Close()
		l = int64(len(b.Bytes()))
	} else {
		b = *bytes.NewBuffer(fb)
		l = f.Size()
	}

	if l <= f.Size() {
		_, err = fmt.Fprintf(file, "var _%s = []byte(\"%s\")\n\n", varN, convert(b.Bytes()))
		_, err = fmt.Fprintf(file, "func %s() []byte {\n return _%s\n }\n\n", varN, varN)

		files[path] = File{varN, l, f.ModTime()}
	}

	return
}

func convert(b []byte) string {
	s := fmt.Sprintf("%#x", b)
	return strings.Replace(s, "0x", "\\x", -1)
}
