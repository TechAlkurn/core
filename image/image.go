package image

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"

	"github.com/TechAlkurn/core/lib"
	"github.com/disintegration/imaging"
)

func Thumb(file string, width int, height int) string {
	dir_cache := os.Getenv("DIR_CACHE")
	dir_storage := os.Getenv("DIR_STORAGE")
	cache_path := os.Getenv("CACHE_PATH")
	storage_path := os.Getenv("STORAGE_PATH")

	thumbs := strings.Split(file, ".")
	thumb := fmt.Sprintf("%s-%d-%d.%s", thumbs[0], width, height, thumbs[len(thumbs)-1])
	cache_thumb := fmt.Sprintf("%s/%s", dir_cache, thumb)
	image_thumb := fmt.Sprintf("%s/%s", cache_path, thumb)
	org_image_thumb := fmt.Sprintf("%s/%s", storage_path, file)
	file = fmt.Sprintf("%s/%s", dir_storage, file)

	if lib.FileIsNotExist(file) {
		thumbs = strings.Split("default.png", ".")
		thumb = fmt.Sprintf("%s-%d-%d.%s", thumbs[0], width, height, thumbs[len(thumbs)-1])
		cache_thumb = fmt.Sprintf("%s/%s", dir_cache, thumb)
		image_thumb = fmt.Sprintf("%s/%s", cache_path, thumb)
		org_image_thumb = fmt.Sprintf("%s/%s", storage_path, file)
		file = fmt.Sprintf("%s/%s", dir_storage, "default.png")
	}

	dir := filepath.Dir(cache_thumb)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0o755)
		if err != nil {
			// log.Fatal(err)
			return org_image_thumb
		}
	}

	if lib.FileIsExist(cache_thumb) {
		return image_thumb
	}
	img, err := imaging.Open(file, imaging.AutoOrientation(true))
	if err != nil {
		// log.Fatal(err)
		return org_image_thumb
	}

	// Fit scales the image down to fit within width x height, preserving
	// aspect ratio. No cropping — the full image is always visible.
	thumbnail := imaging.Fit(img, width, height, imaging.CatmullRom)
	// Center the fitted image on a transparent canvas of the requested size
	// so the output dimensions stay consistent.
	dst := imaging.New(width, height, color.NRGBA{0, 0, 0, 0})
	offset := image.Pt((width-thumbnail.Bounds().Dx())/2, (height-thumbnail.Bounds().Dy())/2)
	dst = imaging.Paste(dst, thumbnail, offset)

	err = imaging.Save(dst, cache_thumb)
	if err != nil {
		// log.Fatal(err)
		return org_image_thumb
	}
	return image_thumb
}
