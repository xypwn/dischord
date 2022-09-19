package config

import (
	"github.com/ulikunitz/xz"

	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

var (
	ErrUnsupportedOSAndArch = errors.New("no download available for your operating system and hardware architecture")
	ErrFileNotFoundInArchive = errors.New("file not found in archive")
	ErrUnsupportedArchive = errors.New("unsupported archive format (supported are .tar, .tar.gz, .tar.xz and .zip")
)

func download(executable bool, urlsByOS map[string]map[string]string, progCallback func(progress float32)) (filename string, err error) {
	// Find appropriate URL
	var url string
	var urlByArch map[string]string
	var ok bool
	if urlByArch, ok = urlsByOS[runtime.GOOS]; !ok {
		urlByArch, ok = urlsByOS["any"]
	}
	if ok {
		if url, ok = urlByArch[runtime.GOARCH]; !ok {
			url, ok = urlByArch["any"]
		}
	}
	if !ok {
		return "", ErrUnsupportedOSAndArch
	}

	// Initiate request
	lastPath := url
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Get the filename to save the downloaded file to
	var savePath string
	if v := resp.Header.Get("Content-Disposition"); v != "" {
		disposition, params, err := mime.ParseMediaType(v)
		if err != nil {
			return "", err
		}
		if disposition == "attachment" {
			lastPath = params["filename"]
		}
	}
	if savePath == "" {
		sp := strings.Split(lastPath, "/")
		savePath = sp[len(sp)-1]
	}

	// Download resource
	size, _ := strconv.Atoi(resp.Header.Get("content-length"))
	var perms uint32 = 0666
	if executable {
		perms = 0777
	}
	file, err := os.OpenFile(savePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fs.FileMode(perms))
	if err != nil {
		return "", err
	}
	for i := 0; ; i += 100_000 {
		_, err := io.CopyN(file, resp.Body, 100_000)
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return "", err
			}
		}
		if progCallback != nil && size != 0 {
			progCallback(float32(i)/float32(size))
		}
	}
	return savePath, nil
}

func unarchiveSingleFile(archive, target string) error {
	unzip := func() error {
		ar, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer ar.Close()

		found := false
		for _, file := range ar.File {
			if !file.FileInfo().IsDir() && filepath.Base(file.Name) == target {
				found = true
				dstFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
				if err != nil {
					return err
				}
				defer dstFile.Close()
				fileReader, err := file.Open()
				if err != nil {
					return err
				}
				defer fileReader.Close()
				if _, err := io.Copy(dstFile, fileReader); err != nil {
					return err
				}
			}
		}
		if !found {
			return ErrFileNotFoundInArchive
		}
		return nil
	}
	untar := func(rd io.Reader) error {
		ar := tar.NewReader(rd)
		for {
			hdr, err := ar.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == target {
				dstFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fs.FileMode(hdr.Mode))
				if err != nil {
					return err
				}
				defer dstFile.Close()
				if _, err := io.Copy(dstFile, ar); err != nil {
					return err
				}
				return nil
			}
		}
		return ErrFileNotFoundInArchive
	}
	match := func(name string, patterns ...string) bool {
		for _, pattern := range patterns {
			matches, err := filepath.Match(pattern, archive)
			if err != nil {
				panic(err)
			}
			if matches {
				return true
			}
		}
		return false
	}
	if match(archive, "*.zip") {
		return unzip()
	} else if match(archive, "*.tar", "*.tar.[gx]z") {
		file, err := os.Open(archive)
		if err != nil {
			return err
		}
		defer file.Close()
		var uncompressedFile io.Reader
		if match(archive, "*.tar") {
			uncompressedFile = file
		} else if match(archive, "*.tar.gz") {
			gz, err := gzip.NewReader(file)
			if err != nil {
				return err
			}
			defer gz.Close()
			uncompressedFile = gz
		} else if match(archive, "*.tar.xz") {
			uncompressedFile, err = xz.NewReader(file)
			if err != nil {
				return err
			}
		}
		return untar(uncompressedFile)
	} else {
		return ErrUnsupportedArchive
	}
	return nil
}
