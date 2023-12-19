package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/djherbis/times"
)

type Photo struct {
	Time time.Time
	Path string
}

func main() {
	// Create a new context
	ctx, cancel := context.WithCancel(context.Background())

	if len(os.Args) != 3 {
		panic("must pass source and dest dir, in that order")
	}

	sourceDir := os.Args[1]
	destDir := os.Args[2]
	if err := os.MkdirAll(destDir, 0777); err != nil {
		panic(err)
	}

	// Check if sourceDir and destDir exist and are directories
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		fmt.Println("Source directory does not exist.")
		return
	}
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		fmt.Println("Destination directory does not exist.")
		return
	}

	// Check if we have appropriate permissions
	if err := os.Chdir(sourceDir); err != nil {
		fmt.Println("Cannot access source directory.")
		return
	}
	if err := os.Chdir(destDir); err != nil {
		fmt.Println("Cannot access destination directory.")
		return
	}

	// Listen for SIGTERM signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
	}()

	// Scan the source directory for image files and extract timestamps
	photos, err := scanDirectoryForImages(sourceDir)
	if err != nil {
		panic(err)
	}
	slices.SortFunc(photos, func(a Photo, b Photo) int {
		return a.Time.Compare(b.Time)
	})

	gap := 3 * time.Hour

	// Copy the photos over
	sessionCount := 0
	copiedCount := 0
	var currentSession string
	for i, photo := range photos {
		// sigterm check
		select {
		case <-ctx.Done():
			fmt.Println("SIGTERM RECEIVED, EXITING...")
			os.Exit(1)
		default:
			// Continue your operation
		}

		// copy the next photo
		var last *Photo
		if i > 0 {
			last = &photos[i-1]
		}

		// Check for a new session
		if currentSession == "" || (last != nil && last.Time.Add(gap).Before(photo.Time)) {
			sessionCount++
			currentSession = fmt.Sprintf("%s/%s", destDir, photo.Time.Format("2006-01-02-15-04-05"))
			fmt.Printf("COPYING SESSION %d [%s]\n", sessionCount, currentSession)

			// Ensure the dest dir
			if err := os.MkdirAll(currentSession, 0777); err != nil {
				panic(err)
			}
		}

		// // XXX: SKIP ALL BUT THE Xth SESSION
		// if sessionCount != 6 {
		// 	continue
		// }

		// Copy the photo
		dest := filepath.Join(currentSession, filepath.Base(photo.Path))
		fmt.Printf("\t%d:%d: [%s] => [%s] (%s)\n", i+1, len(photos), photo.Path, dest, photo.Time.Format("2006-01-02-15-04-05"))
		copyFile(photo.Path, dest)
		copiedCount++
	}

	fmt.Println("DONE!")
}

func scanDirectoryForImages(dirPath string) ([]Photo, error) {
	var photos []Photo
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && isImageFile(path) {
			timeStat, err := times.Stat(path)
			if err != nil {
				return err
			}
			time := timeStat.ModTime()
			if timeStat.HasBirthTime() {
				time = timeStat.BirthTime()
			}

			photos = append(photos, Photo{
				Time: time,
				Path: path,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return photos, nil
}

func isImageFile(filePath string) bool {
	// Add more image formats if needed
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".jpg", ".jpeg", ".png", ".nef":
		return true
	}
	return false
}

func copyFile(src, dst string) {
	_, err := os.Stat(dst)
	if err == nil {
		fmt.Println("\t    * Destination file already exists. Skipping...")
		return
	}
	if !os.IsNotExist(err) {
		fmt.Println("Error retrieving destination file info:", err)
		return
	}

	sourceFile, err := os.Open(src)
	if err != nil {
		fmt.Println("Error opening source file:", err)
		return
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		fmt.Println("Error creating destination file:", err)
		return
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		fmt.Println("Error copying file:", err)
	}
}
