package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"sync"

	mapbox "github.com/ryankurte/go-mapbox/lib"
	"github.com/ryankurte/go-mapbox/lib/maps"
	"github.com/sjsafranek/goutils/shell"
)

// xyz
type xyz struct {
	x uint64
	y uint64
	z uint64
}

func NewTerrainMap(token string) (*TerrainMap, error) {
	mb, err := mapbox.NewMapbox(MAPBOX_TOKEN)
	return &TerrainMap{MapBox: mb, zoom: 2}, err
}

type TerrainMap struct {
	MapBox *mapbox.Mapbox
	zoom   int
}

func (tm *TerrainMap) SetZoom(zoom int) {
	tm.zoom = zoom
}

func ResolveMapType(mapType string) maps.MapID {
	switch mapType {
	case "satellite":
		return maps.MapIDSatellite
	case "terrain":
		return maps.MapIDTerrainRGB
	case "streets":
		return maps.MapIDStreets
	default:
		panic(errors.New("Unsupported map type"))
	}
}

func (tm *TerrainMap) Render(minLat, maxLat, minLng, maxLng float64, zoom int, outFile string, mapType string) {
	mt := ResolveMapType(mapType)
	tiles := GetTileNamesFromMapView(minLat, maxLat, minLng, maxLng, zoom)

	log.Printf(`Parameters:
	extent:	[%v, %v, %v, %v]
	zoom:	%v
	tiles:	%v`, minLat, maxLat, minLng, maxLng, zoom, len(tiles))

	if 300 < len(tiles) {
		panic(errors.New("too many map tiles. Please raise map zoom or change bounds"))
	}

	// create temp directroy
	directory, err := os.MkdirTemp(os.TempDir(), "terrain-rgb")
	if nil != err {
		panic(err)
	}
	defer os.RemoveAll(directory)
	//.end

	var workwg sync.WaitGroup
	queue := make(chan xyz, numWorkers*2)

	log.Println("Spawning workers")
	for i := 0; i < numWorkers; i++ {
		go terrainWorker(tm.MapBox, queue, directory, mt, &workwg)
	}

	log.Println("Requesting tiles")
	for _, v := range tiles {
		workwg.Add(1)
		queue <- v
	}

	close(queue)

	workwg.Wait()

	log.Println("Building GeoTIFF")
	err = tm.buildGeoTIFF(directory, outFile)
	if nil != err {
		log.Fatal(err)
	}
}

func (tm TerrainMap) buildGeoTIFF(directory, outFile string) error {
	// bash script contents
	script := `
#!/bin/bash

DIRECTORY=$1
OUT_FILE=$2

# build tiff from each file
echo "Building tif files from csv map tiles"
for FILE in $DIRECTORY/*.csv; do
    GEOTIFF="${FILE%.*}.tif"
    XYZ="${FILE%.*}.xyz"
	#GEOTIFF=${FILE/.csv/.tif}
    #XYZ=${FILE/.csv/.xyz}

    echo "Building $XYZ from $FILE"
    $(echo head -n 1 $FILE) >  "$XYZ"; \
        tail -n +2 $FILE | sort -n -t ',' -k2 -k1 >> "$XYZ";

    echo "Building $GEOTIFF from $XYZ"
    gdal_translate "$XYZ" "$GEOTIFF"
done

echo "Merging tif files to $OUT_FILE"
gdalwarp --config GDAL_CACHEMAX 3000 -wm 3000 $DIRECTORY/*.tif $OUT_FILE
	`

	// write to bash script
	fh, err := os.CreateTemp("", "build_geotiff.*.sh")
	if nil != err {
		return err
	}
	fmt.Fprint(fh, script)
	fh.Close()
	defer os.Remove(fh.Name())

	// execute bash script
	log.Println(directory, outFile)
	shell.RunScript("/bin/sh", fh.Name(), directory, outFile)

	return nil
}
