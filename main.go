package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/kelindar/dbscan"
)

type Photo struct {
	Time time.Time
	Path string
}

func (p Photo) DistanceTo(other dbscan.Point) float64 {
	mt := p.Time
	opt := other.(Photo)
	ot := opt.Time
	dt := mt.Sub(ot)
	return math.Abs(dt.Seconds())
}

func (p Photo) Name() string {
	return (p.Time).Format(time.DateTime)
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
	photos := scanDirectoryForImages(sourceDir)

	// Convert timestamps to DBSCAN points
	points := make([]dbscan.Point, 0)
	for _, t := range photos {
		points = append(points, t)
	}

	// Run DBSCAN
	epsilon := (4 * time.Hour).Seconds() // 1 minute in seconds, adjust as needed
	minPoints := 2                       // minimum number of points to form a cluster
	clusters := dbscan.Cluster(minPoints, epsilon, points...)

	// Copy files based on clusters
	for i, cluster := range clusters {
		// While scanning the directory and processing the images, periodically check if ctx is done:

		select {
		case <-ctx.Done():
			fmt.Println("SIGTERM RECEIVED, EXITING...")
			os.Exit(1)
		default:
			// Continue your operation
		}

		fmt.Printf("COPYING CLUSTER %d OF %d...\n", i+1, len(clusters))

		// Sort each photo entry by time
		slices.SortFunc(cluster, func(a dbscan.Point, b dbscan.Point) int {
			pa := a.(Photo)
			pb := b.(Photo)
			return pa.Time.Compare(pb.Time)
		})

		// Grab the first entry, to name the group
		firstPhoto := cluster[0].(Photo)

		clusterDir := fmt.Sprintf("%s/%s", destDir, firstPhoto.Time.Format("2006-01-02-15-04-05"))
		if err := os.MkdirAll(clusterDir, 0777); err != nil {
			panic(err)
		}

		for j, point := range cluster {
			photo := point.(Photo)
			dest := filepath.Join(clusterDir, filepath.Base(photo.Path))

			fmt.Printf("\t%d:%d %d:%d: [%s] => [%s]\n", i+1, len(clusters), j+1, len(cluster), photo.Path, dest)
			copyFile(photo.Path, dest)
		}
	}

	fmt.Println("DONE!")
}

func scanDirectoryForImages(dirPath string) []Photo {
	photos := make([]Photo, 0)
	filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && isImageFile(path) {
			photos = append(photos, Photo{
				Time: info.ModTime(),
				Path: path,
			})
		}
		return nil
	})
	return photos
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
