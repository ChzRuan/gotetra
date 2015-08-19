package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path"
	"sort"
	"time"
	"runtime/pprof"

	"github.com/phil-mansfield/table"
	"github.com/phil-mansfield/gotetra/render/io"	
	"github.com/phil-mansfield/gotetra/render/halo"

	"github.com/phil-mansfield/gotetra/los"
	"github.com/phil-mansfield/gotetra/los/geom"
	"github.com/phil-mansfield/gotetra/los/analyze"

	plt "github.com/phil-mansfield/pyplot"
)

const (
	rType = halo.R200m
	rMaxMult = 3.0
	rMinMult = 0.5

	n = 1024
	bins = 256

	rings = 6
)

var (
	colors = []string{
		"DarkSlateBlue", "DarkSlateGray", "DarkTurquoise",
		"DarkViolet", "DeepPink", "DimGray",
	}
)

func main() {
	fmt.Println("Running")
	if len(os.Args) != 4 {
		log.Fatalf("Usage: $ %s input_dir halo_file plot_dir", os.Args[0])
	}

	dirName := os.Args[1]
	haloFileName := os.Args[2]
	plotDir := os.Args[3]

	files, err := fileNames(dirName)
	if err != nil { log.Fatal(err.Error()) }
	hds := make([]io.SheetHeader, len(files))
	for i := range files {
		if i % 50 == 0 { fmt.Print(i, " ") }
		err = io.ReadSheetHeaderAt(files[i], &hds[i])
		if err != nil { log.Fatal(err.Error()) }
	}

	xs, ys, zs, ms, rs, err := readHalos(haloFileName, &hds[0].Cosmo)
	if err != nil { log.Fatal(err.Error()) }
	fmt.Printf("%d halos read.\n", len(xs))

	buf := los.NewBuffers(files[0], &hds[0])
	h := new(los.HaloProfiles)
	f, err := os.Create("out.pprof")
	if err != nil { log.Fatal(err.Error()) }
	pprof.StartCPUProfile(f)
	defer pprof.StopCPUProfile()

	plotRs, plotRhos := make([]float64, bins), make([]float64, bins)
	for _, i := range []int{1000, 1001, 1002, 1003, 1004} {
		origin := &geom.Vec{float32(xs[i]), float32(ys[i]), float32(zs[i])}
		h.Init(i, rings, origin, rs[i] * rMinMult, rs[i] * rMaxMult,
			bins, n, los.Log(false))
		hdIntrs, fileIntrs := intersectingSheets(h, hds, files)

		fmt.Printf(
			"Halo mass is: %.3g, intersects are: %d\n", ms[i], len(hdIntrs),
		)

		intersectionTest(h, hdIntrs, fileIntrs, buf)
		plotExampleProfiles(h, plotRs, plotRhos, plotDir)
	}

	plt.Execute()
}

func plotExampleProfiles(hp *los.HaloProfiles, rs, rhos []float64, dir string) {
	fname := path.Join(dir, fmt.Sprintf("profs_%dh.png", hp.ID()))

	plt.Figure()
	hp.GetRs(rs)

	r := rs[len(rs) - 1] / rMaxMult
	plt.Plot([]float64{r, r}, []float64{1e-2, 1e3}, "k", plt.LW(2))

	for ring := 0; ring < hp.Rings(); ring++ {
		hp.GetRhos(ring, rand.Intn(hp.Profiles()), rhos)
		rhoSets, auxSets := analyze.NaNSplit(rhos, analyze.Aux(rs))

		for i := range rhoSets {
			plotRs, plotRhos := auxSets[0][i], rhoSets[i]
			plt.Plot(plotRs, plotRhos, plt.LW(3), plt.C(colors[ring]))
		}
	}

	
	plt.Title(fmt.Sprintf("Halo %d", hp.ID()))
	plt.XLabel(`$R$ $[{\rm Mpc}/h]$`, plt.FontSize(16))
	plt.YLabel(`$\rho$ [$\rho_m$]`, plt.FontSize(16))

	plt.XScale("log")

	plt.YScale("log")
	plt.YLim(1e-2, 1e3)

	plt.Grid(plt.Axis("y"))
	plt.Grid(plt.Axis("x"), plt.Which("both"))
	plt.SaveFig(fname)
}

// fileNames returns the names of all the files in a directory.
func fileNames(dirName string) ([]string, error) {
	infos, err := ioutil.ReadDir(dirName)
	if err != nil { return nil, err }

	files := make([]string, len(infos))
	for i := range infos {
		files[i] = path.Join(dirName, infos[i].Name())
	}
	return files, nil
}

// halos allows for arrays of halo properties to be sorted simultaneously.
type halos struct {
	xs, ys, zs, ms, rs []float64
}

func (hs *halos) Len() int { return len(hs.rs) }
func (hs *halos) Less(i, j int) bool { return hs.rs[i] < hs.rs[j] }
func (hs *halos) Swap(i, j int) {
	hs.rs[i], hs.rs[j] = hs.rs[j], hs.rs[i]
	hs.ms[i], hs.ms[j] = hs.ms[j], hs.ms[i]
	hs.xs[i], hs.xs[j] = hs.xs[j], hs.xs[i]
	hs.ys[i], hs.ys[j] = hs.ys[j], hs.ys[i]
	hs.zs[i], hs.zs[j] = hs.zs[j], hs.zs[i]
}

// readHalos reads halo information from the given Rockstar catalog.
func readHalos(
	file string, cosmo *io.CosmologyHeader,
) (xs, ys, zs, ms, rs []float64, err error) {
	rCol := rType.RockstarColumn()
	xCol, yCol, zCol := 17, 18, 19
	
	colIdxs := []int{ xCol, yCol, zCol, rCol }
	cols, err := table.ReadTable(file, colIdxs, nil)
	if err != nil { return nil, nil, nil, nil, nil, err }
	
	xs, ys, zs = cols[0], cols[1], cols[2]
	if rType.RockstarMass() {
		ms = cols[3]
		rs = make([]float64, len(ms))
		rType.Radius(cosmo, ms, rs)
	} else {
		rs = cols[3]
		ms = make([]float64, len(rs))
		for i := range rs { rs[i] /= 1000 } // kpc -> Mpc
		rType.Mass(cosmo, rs, ms)
	}

	sort.Sort(sort.Reverse(&halos{ xs, ys, zs, ms, rs }))
	return xs, ys, zs, ms, rs, nil
}

// intersectingSheets returns all the SheetHeaders and file names that intersect
// with a given halo.
func intersectingSheets(
	h *los.HaloProfiles, hds []io.SheetHeader, files []string,
) ([]io.SheetHeader, []string) {
	hdOuts, fileOuts := []io.SheetHeader{}, []string{}
	for i := range hds {
		if h.SheetIntersect(&hds[i]) {
			hdOuts = append(hdOuts, hds[i])
			fileOuts = append(fileOuts, files[i])
		}
	}
	return hdOuts, fileOuts
}

// intersectionTest counts how many candidate intersections we have by:
// (a). Only considering tetrahedra with points insde the halo.
// (b). Conisdering tetrahedra whose bounding sphere intersects the halo.
func intersectionTest(
	h *los.HaloProfiles, hds []io.SheetHeader, files []string, buf *los.Buffers,
) {
	hs := []los.HaloProfiles{*h}
	h = &hs[0]

	for i, file := range files {
		hd := &hds[i]
		fmt.Printf("    Reading %s -> ", path.Base(file))
		los.WrapHalo(hs, hd)

		t1 := float64(time.Now().UnixNano())
		buf.ParallelRead(file, hd)
		t2 := float64(time.Now().UnixNano())
		buf.ParallelDensity(h)
		t3 := float64(time.Now().UnixNano())

		fmt.Printf("Setup: %.3g s  Density: %.3g s\n",
			(t2 - t1) / 1e9, (t3 - t2) / 1e9)
	}
	fmt.Printf("    Rho: %.3g\n", h.Rho())
}
