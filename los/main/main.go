package main

import (
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"os"
	"path"
	"strings"
	"runtime/pprof"

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

	n = 124
	bins = 256
	window = 121
	cutoff = 0.0

	rings = 25
	plotStart = 8
	plotCount = 1

	I, J = 3, 3
	
	// SubhaloFinder params.
	finderCells = 150
	overlapMult = 3

	hdSaveFile = "hdSave.dat"
)

var (
	colors = []string{
		"DarkSlateBlue", "DarkSlateGray", "DarkTurquoise",
		"DarkViolet", "DeepPink", "DimGray",
	}
	refRings = []int{
		25, //10, 10, 10, 10, 10,
		//20, 20, 20, 20, 20, 20,
		//20, 20, 20, 20, 20, 20,
		//20, 20, 20, 20, 20, 20,
		//20, 20, 20, 20, 20, 20,
		//20, 20, 20, 20, 20, 20,
		//40, 40, 40, 40, 40, 40,
		//40, 40, 40, 40, 40, 40,
		//40, 40, 40, 40, 40, 40,
		//40, 40, 40, 40, 40, 40,
		//3, 4, 6, 10,
	}
	refHalos = len(refRings)
	visProfs = []int{
		rand.Intn(n), rand.Intn(n), rand.Intn(n),
	//	rand.Intn(n), rand.Intn(n), rand.Intn(n),
	}
)

func loadHeaders(files []string, saveDir string) ([]io.SheetHeader, error) {
	saveFile := path.Join(saveDir, hdSaveFile)
	hds := make([]io.SheetHeader, len(files))

	if _, err := os.Stat(saveFile); err == nil {
		f, err := os.Open(saveFile)
		if err != nil { return nil, err }
		defer f.Close()
		fmt.Println("Loading saved headers.")
		binary.Read(f, binary.LittleEndian, hds)
		
	} else {
		fmt.Print("Loading individual headers: ")

		for i := range files {
			if i % 50 == 0 { fmt.Print(i, " ") }
			err = io.ReadSheetHeaderAt(files[i], &hds[i])
			if err != nil { return nil, err }
		}

		f, err := os.Create(saveFile)
		if err != nil { return nil, err }
		defer f.Close()
		binary.Write(f, binary.LittleEndian, hds)
		fmt.Println()

	}
	return hds, nil
}

func main() {
	// Argument Parsing.
	fmt.Println("Running")
	if len(os.Args) != 6 {
		log.Fatalf("Usage: $ %s input_dir halo_file plot_dir text_dir save_dir",
			os.Args[0])
	}

	dirName := os.Args[1]
	haloFileName := os.Args[2]
	plotDir := os.Args[3]
	textDir := os.Args[4]
	saveDir := os.Args[5]

	// Do I/O and set up buffers.
	files, err := fileNames(dirName)
	if err != nil { log.Fatal(err.Error()) }
	hds, err := loadHeaders(files, saveDir)
	if err != nil { log.Fatal(err.Error()) }
	buf := los.NewBuffers(files[0], &hds[0])
	fmt.Println("Loaded headers")

	// Find halos, subhalos, etc.
	rids, xs, ys, zs, ms, rs, err := halo.ReadRockstar(
		haloFileName, rType, &hds[0].Cosmo,
	)

	if err != nil { log.Fatal(err.Error()) }
	fmt.Println("Read halos")
	g := halo.NewGrid(finderCells, hds[0].TotalWidth, len(xs))
	g.Insert(xs, ys, zs)
	sf := halo.NewSubhaloFinder(g)
	sf.FindSubhalos(xs, ys, zs, rs, overlapMult)
	fmt.Println("Found subhalos")

	// Profiling boilerplate.
	f, err := os.Create("out.pprof")
	if err != nil { log.Fatal(err.Error()) }
	pprof.StartCPUProfile(f)
	defer pprof.StopCPUProfile()

	// Analyze each halo.
	plotRs, plotRhos := make([]float64, bins), make([]float64, bins)

	totRbs := make([][]analyze.RingBuffer, refHalos + 1)
	totRbs[0] = make([]analyze.RingBuffer, rings)
	rbs := totRbs[0]
	rbRefs := totRbs[1:]
	for j := range rbRefs {
		rbRefs[j] = make([]analyze.RingBuffer, refRings[j])
	}

	for j := range totRbs {
		for i := range totRbs[j] {
			totRbs[j][i].Init(n, bins)
		}
	}

	//idx := -1
	//for j, rid := range rids {
	//	if rid == 166305652 { idx = j }
	//}
	//if idx == -1 {
	//	panic("Couldn't find it.")
	//}

	//for i := plotStart; i < plotStart + plotCount; i++ {
	//for _, i := range []int{
	//	1001, 1006, 1008, 1009, 1014, 1017, 1018, 1033, 1047, 6006, 6030,
	//} {

	ranks := []int{}
	switch hds[0].TotalWidth {
	case 62.5:
		ranks = []int{ 4302, 8092, 6862, 522, 4565, 2991, 4250 }
	case 125.0:
		ranks = []int{ 2019, 2189, 1568, 1268, 367, 1673, 276, 300 }
	case 250.0:
		ranks = []int{ 296, 201, 314, 236, 924, 1477, 521, 726, 339 }
	case 500.0:
		ranks = []int{ 8, 51, 105, 825, 465, 562, 902, 809, 241 }
	default:
		panic(fmt.Sprintf("recognized box with %g", hds[0].TotalWidth))
	}

	for _, i := range ranks {
		fmt.Printf("Loading %d (%d)\n", i, rids[i])
		if sf.HostCount(i) > 0 { 
			fmt.Println("Ignoring halo with host.")
			continue
		}
		
		origin := &geom.Vec{float32(xs[i]), float32(ys[i]), float32(zs[i])}

		hs := make([]los.HaloProfiles, refHalos + 1)
		h := &hs[0]
		hRefs := hs[1:]

		h.Init(i, rings, origin, rs[i] * rMinMult, rs[i] * rMaxMult,
			bins, n, hds[0].TotalWidth, los.Log(true))
		for j := range hRefs {
			hRefs[j].Init(i, refRings[j], origin, rs[i] * rMinMult,
				rs[i] * rMaxMult, bins, n, hds[0].TotalWidth, los.Log(true),
				los.Rotate(float32(2 * math.Pi * rand.Float64()),
					float32(2 * math.Pi * rand.Float64()),
					float32(2 * math.Pi * rand.Float64())))
		}
		hdIntrs, fileIntrs := intersectingSheets(h, hds, files)		
		
		fmt.Println("Computing Densities")
		los.LoadDensities(hs, hdIntrs, fileIntrs, buf)
		for j := range totRbs {
			for k := range totRbs[j] {
				totRbs[j][k].Clear()
				totRbs[j][k].Splashback(&hs[j], k, window, cutoff)
			}
		}

		fmt.Println("Single Fit")
		pShells := make([]analyze.ProjectedShell, len(hRefs))
		shells := make([]analyze.Shell, len(hRefs))
		
		for j := range pShells {
			pxs, pys, _ := analyze.FilterPoints(rbRefs[j], 3) 
			cs, pShell := analyze.PennaPlaneFit(pxs, pys, &hRefs[j], I, J)
			shell := analyze.PennaFunc(cs, I, J, 2)
			v := shell.Volume(100 * 1000)
			vRad := math.Pow(v / (4 * math.Pi / 3), 0.3333)
			PrintCoeffs(cs, vRad, rs[i])
			pShells[j], shells[j] = pShell, shell
		}
		pxs, pys, _ := analyze.FilterPoints(rbs, 3) 
		cs, _ := analyze.PennaPlaneFit(pxs, pys, h, I, J)
		_ = cs

		for j, shell := range shells {
			printShellStats(shell, h.ID(), j, 10 * 1000)
		}

		//for j := range rbRefs {
		//	plotKde(rbRefs[j], ms[i], h.ID(), j, plotDir)
		//}

		sh := shells[0]
		v := sh.Volume(100 * 1000)
		vRad := math.Pow(v / (4 * math.Pi / 3), 0.3333)
		
		rMin, rMax := sh.RadialRange(100 * 1000)
		cv := sh.CartesianSampledVolume(100 * 1000, rMax)
		cvRad := math.Pow(cv / (4 * math.Pi / 3), 0.3333)
		meanR := sh.MeanRadius(100 * 1000)
		medR := sh.MedianRadius(100 * 1000)

		fmt.Printf("%8s %8s %8s %8s %8s %8s\n",
			"rMin", "rMax", "vRad", "cvRad", "meanR", "medR")
		fmt.Printf("%8.3g %8.3g %8.3g %8.3g %8.3g %8.3g\n",
			rMin, rMax, vRad, cvRad, meanR, medR)
		
		fmt.Println("Plotting Tracers")
		//plotTracers(hRefs, rbRefs, h.ID(), 1, 1000, plotDir)
		fmt.Println("Plotting Plane")
		for ring := 0; ring < rings; ring++ {
			plotPlane(h, &rbs[ring], ms[i], h.ID(),
				ring, pShells, medR, plotDir, textDir)
			_, _ = plotRhos, plotRs
			//plotExampleProfiles(h, ms[i], ring, plotRs, plotRhos, plotDir)
			//plotExampleDerivs(h, ms[i], ring, plotRs, plotRhos, plotDir)
		}
	}
	
	plt.Execute()
}

func PrintCoeffs(cs []float64, rsp, r200m float64) {
	fmt.Printf("-1 -1 %g %g ", rsp, r200m)
	for _, c := range cs {
		fmt.Print(c, " ")
	}
	fmt.Println()
}

func subhaloSpheres(
	sf *halo.SubhaloFinder, i int, xs, ys, zs, rs []float64,
) []geom.Sphere {
	shIdxs := sf.Subhalos(i)
	subhalos := make([]geom.Sphere, len(shIdxs))

	for j, idx := range shIdxs {
		subhalos[j].R = float32(rs[idx])
		subhalos[j].C = geom.Vec{
			float32(xs[idx]), float32(ys[idx]), float32(zs[idx]),
		}
	}
	return subhalos
}

func plotExampleProfiles(
	hp *los.HaloProfiles, m float64, ring int,
	rs, rhos []float64, dir string,
) {
	fname := path.Join(dir, fmt.Sprintf("profs_h%d_r%d.png", hp.ID(), ring))

	//plt.Figure()
	plt.InsertLine("plt.clf()")
	hp.GetRs(rs)

	r := rs[len(rs) - 1] / rMaxMult
	plt.Plot([]float64{r, r}, []float64{1e5, 0.01}, "k", plt.LW(2))

	for cIdx, visIdx := range visProfs {
		hp.GetRhos(ring, visIdx, rhos)
		rhoSets, auxSets := analyze.NaNSplit(rhos, analyze.Aux(rs))
		
		for i := range rhoSets {
			rawRs, rawRhos := auxSets[0][i], rhoSets[i]
			smoothRhos, smoothDerivs, ok := analyze.Smooth(
				rawRs, rawRhos, window,
			)
			if !ok { continue }
			//plt.Plot(rawRs, smoothRhos, plt.LW(3),
			//	plt.C(colors[cIdx % len(colors)]))
			plt.Plot(rawRs, rawRhos, plt.LW(3), plt.C(colors[cIdx % len(colors)]))
			r, ok := analyze.SplashbackRadius(rawRs, smoothRhos, smoothDerivs)
			if !ok { continue }
			_ = r
			//plt.Plot([]float64{r, r}, []float64{1e5, 0.01},
			//	plt.C(colors[cIdx % len(colors)]), plt.LW(3))
		}
	}

	// Plot specifications.
	//plt.Title(fmt.Sprintf(
	//	`Halo %d: $M_{\rm 200m}$ = %.3g $M_\odot/h$`, hp.ID(), m),
	//)
	plt.XLabel(`$R$ $[{\rm Mpc}/h]$`, plt.FontSize(19))
	plt.YLabel(`$\rho$ [$\rho_m$]`, plt.FontSize(19))

	plt.XScale("log")

	plt.YScale("log")
	plt.YLim(1e-2, 1e3)
	setXRange(rs[0], rs[len(rs) - 1])

	plt.Grid(plt.Axis("y"))
	plt.Grid(plt.Axis("x"), plt.Which("both"))
	plt.SaveFig(fname)
}

func plotExampleDerivs(
	hp *los.HaloProfiles, m float64, ring int,
	rs, rhos []float64, dir string,
) {
	fname := path.Join(dir, fmt.Sprintf("derivs_h%d_r%d.png", hp.ID(), ring))

	plt.InsertLine("plt.clf()")
	hp.GetRs(rs)

	r := rs[len(rs) - 1] / rMaxMult
	plt.Plot([]float64{r, r}, []float64{-20, +10}, "k", plt.LW(2))


	for cIdx, visIdx := range visProfs {
		hp.GetRhos(ring, visIdx, rhos)
		rhoSets, auxSets := analyze.NaNSplit(rhos, analyze.Aux(rs))
		for i := range rhoSets {
			rawRs, rawRhos := auxSets[0][i], rhoSets[i]
			smoothRhos, smoothDerivs, ok := analyze.Smooth(
				rawRs, rawRhos, window,
			)

			if !ok { continue }
			plt.Plot(rawRs, smoothDerivs, plt.LW(3),
				plt.C(colors[cIdx % len(colors)]))
			r, ok := analyze.SplashbackRadius(rawRs, smoothRhos, smoothDerivs)

			if !ok { continue }
			plt.Plot([]float64{r, r}, []float64{-20, +10},
				plt.C(colors[cIdx % len(colors)]))
		}
	}

	// Plot specifications.
	plt.Title(fmt.Sprintf(
		`Halo %d: $M_{\rm 200m}$ = %.3g $M_\odot/h$`, hp.ID(), m),
	)
	plt.XLabel(`$R$ $[{\rm Mpc}/h]$`, plt.FontSize(16))
	plt.YLabel(`$d \ln{\rho}/ d\ln{r}$ [$\rho_m$]`, plt.FontSize(16))

	plt.XScale("log")
	plt.YLim(-20, +10)
	// plt.YLim(-2, +1)
	setXRange(rs[0], rs[len(rs) - 1])

	plt.Grid(plt.Axis("y"))
	plt.Grid(plt.Axis("x"), plt.Which("both"))
	plt.SaveFig(fname)
}

func plotKde(rbs []analyze.RingBuffer, m float64, id, rot int, plotDir string) {
	fName := path.Join(plotDir, fmt.Sprintf("kde_h%d_rot%d", id, rot))

	plt.Figure(plt.Num(1), plt.FigSize(8, 8))
	plt.InsertLine("plt.clf()")

	n := len(rbs[0].Oks)
	validRs, validPhis := make([]float64, 0, n), make([]float64, 0, n)
	for i := range rbs {
		r := &rbs[i]
		validRs, validPhis = r.OkPolarCoords(validRs, validPhis)
		kt, _ := analyze.NewKDETree(validRs, validPhis, 1, 10)
		kt.PlotLevel(0, plt.C(colors[rot % len(colors)]), plt.LW(3))
	}

	plt.Title(fmt.Sprintf(`Halo %d: $M_{\rm 200c}$ = %.3g $M_\odot/h$`, id, m))
	plt.XLabel(`$r$ [{\rm Mpc}/$h$]`)
	plt.SaveFig(fName)
}

func plotPlane(
	h *los.HaloProfiles, r *analyze.RingBuffer, m float64, id, ring int,
	pShells []analyze.ProjectedShell, rad float64, plotDir, textDir string,
) {
	pName := path.Join(plotDir, fmt.Sprintf("plane_h%d_r%d.png", id, ring))
	xs, ys := make([]float64, 0, r.N), make([]float64, 0, r.N)
	rs, phis := make([]float64, 0, r.N), make([]float64, 0, r.N)

	xs, ys = r.OkPlaneCoords(xs, ys)
	rs, phis = r.OkPolarCoords(rs, phis)
	kt, _ := analyze.NewKDETree(rs, phis, 4, 10)

	fRs, fThs, _ := kt.FilterNearby(rs, phis, 4, kt.H() / 2)
	fXs, fYs := make([]float64, len(fRs)), make([]float64, len(fRs))
	for i := range fRs {
		sin, cos := math.Sincos(fThs[i])
		fXs[i], fYs[i] = fRs[i] * cos, fRs[i] * sin
	}

	plt.Figure(plt.Num(1), plt.FigSize(8, 8))
	plt.InsertLine("plt.clf()")
	plt.Plot(xs, ys, "ow")
	
	rf := kt.GetRFunc(4, analyze.Radial)
	spXs, radXs := make([]float64, 200), make([]float64, 200)
	spYs, radYs := make([]float64, 200), make([]float64, 200)
	dPhi := 2 * math.Pi / float64(len(spXs) - 1)
	for i := range spXs {
		phi := (float64(i) + 0.5) * dPhi
		r := rf(phi)
		sin, cos := math.Sincos(phi)
		spXs[i], spYs[i] = r * cos, r * sin
		radXs[i], radYs[i] = rad * cos, rad * sin
	}

	spXs[len(spXs) - 1], spYs[len(spYs) - 1] = spXs[0], spYs[0]
	plt.Plot(spXs, spYs, plt.Color("r"), plt.LW(2))
	plt.Plot(fXs, fYs, "o", plt.Color("r"))
	plt.Plot(radXs, radYs, plt.Color("g"), plt.LW(2))
	
	for i, pShell := range pShells {
		rXs, rYs := make([]float64, 100), make([]float64, 100)
		for i := range rXs {
			phi := float64(i) * 2 * math.Pi / float64(len(rXs) - 1)
			r := pShell(h, ring, phi)
			sin, cos := math.Sincos(phi)
			rXs[i], rYs[i] = r * cos, r * sin
		}
		rXs[len(rXs)-1], rYs[len(rYs)-1] = rXs[0], rYs[0]
		plt.Plot(rXs, rYs, plt.C(colors[i % len(colors)]), plt.LW(2))
	}

	// Plot the colored profiles.
	for i := 0; i < r.N; i++ {
		if r.Oks[i] {
			for visIdx, j := range visProfs {
				if j == i { 
					plt.Plot(
						[]float64{r.PlaneXs[i]}, []float64{r.PlaneYs[i]},
						"o", plt.Color(colors[visIdx % len(colors)]),
					)
				}
			}
		}
	}

	
	plt.Title(fmt.Sprintf(`Halo %d: $M_{\rm 200m}$ = %.3g $M_\odot/h$`, id, m))
	plt.XLabel(`$X_1$ $[{\rm Mpc}/h]$`, plt.FontSize(16))
	plt.YLabel(`$X_2$ $[{\rm Mpc}/h]$`, plt.FontSize(16))
	rMax := 0.0
	for _, r := range r.Rs {
		if r > rMax { rMax = r }
	}

	plt.XLim(-rMax, +rMax)
	plt.YLim(-rMax, +rMax)
	plt.SaveFig(pName)
}

func plotTracers(
	hs []los.HaloProfiles, rbs [][]analyze.RingBuffer,
	id, step, samples int, plotDir string,
) {
	linName := path.Join(plotDir, fmt.Sprintf("trace_h%d_lin.png", id))
	logName := path.Join(plotDir, fmt.Sprintf("trace_h%d_log.png", id))

	// Set up the cumulative shell measures.
	start := 10
	stop := len(rbs[0])
	shells, ringCounts := [][]analyze.Shell{}, []int{}
	for ih := range hs {
		h := &hs[ih]
		xs, ys, _ := analyze.FilterPoints(rbs[ih], 4)
		hRingCounts, hShells := analyze.CumulativeShells(
			xs, ys, h, I, J, start, stop, step,
		)
		ringCounts = hRingCounts
		shells = append(shells, hShells)
	}

	means, stds := analyze.CumulativeTracers(shells, samples)
	n := len(means)

	vols, mvols := make([]float64, n), make([]float64, n)
	sas, msas := make([]float64, n), make([]float64, n)
	ixs, mixs := make([]float64, n), make([]float64, n)
	iys, miys := make([]float64, n), make([]float64, n)
	izs, mizs := make([]float64, n), make([]float64, n)
	for i := 0; i < n; i++ {
		mvols[i] = means[i].Vol
		msas[i] = means[i].Sa
		mixs[i] = means[i].Ix
		miys[i] = means[i].Iy
		mizs[i] = means[i].Iz

		vols[i] = stds[i].Vol / means[i].Vol
		sas[i] = stds[i].Sa / means[i].Sa
		ixs[i] = stds[i].Ix / means[i].Ix
		iys[i] = stds[i].Iy / means[i].Iy
		izs[i] = stds[i].Iz / means[i].Iz
	}

	plt.Figure(plt.Num(1), plt.FigSize(8, 8))
	plt.InsertLine("plt.clf()")

	plt.Plot(ringCounts, vols, "r", plt.LW(3), plt.Label("Volume"))
	plt.Plot(ringCounts, sas, "b", plt.LW(3), plt.Label("Surface Area"))
	plt.Plot(ringCounts, ixs, "g", plt.LW(3), plt.Label(`$I_{\rm x}$`))
	plt.Plot(ringCounts, iys, "purple", plt.LW(3), plt.Label(`$I_{\rm y}$`))
	plt.Plot(ringCounts, izs, "orange", plt.LW(3), plt.Label(`$I_{\rm z}$`))

	plt.Legend()

	plt.XLabel("Ring Count", plt.FontSize(16))
	plt.YLabel(`${\rm std}(X) / {\rm mean}(X)$`)
	plt.YLim(0, nil)

	plt.SaveFig(linName)

	plt.Figure(plt.Num(1), plt.FigSize(8, 8))
	plt.InsertLine("plt.clf()")

	plt.Plot(ringCounts, vols, "r", plt.LW(3), plt.Label("Volume"))
	plt.Plot(ringCounts, sas, "b", plt.LW(3), plt.Label("Surface Area"))
	plt.Plot(ringCounts, ixs, "g", plt.LW(3), plt.Label(`$I_{\rm x}$`))
	plt.Plot(ringCounts, iys, "purple", plt.LW(3), plt.Label(`$I_{\rm y}$`))
	plt.Plot(ringCounts, izs, "orange", plt.LW(3), plt.Label(`$I_{\rm z}$`))

	plt.Legend()

	plt.XLabel("Ring Count", plt.FontSize(16))
	plt.YLabel(`${\rm std}(X) / {\rm mean}(X)$`)

	plt.XScale("log")
	plt.YScale("log")

	plt.SaveFig(logName)
}

func setXRange(xLow, xHigh float64) {
	if (xLow < 1 && xHigh  > 1) ||
		(xLow < 0.1 && xHigh > 0.1) || 
		(xLow < 0.01 && xHigh > 0.01) {
		plt.XLim(xLow, xHigh)
	}
}

func strSlice(xs []float64) string {
	tokens := make([]string, len(xs))
	for i := range tokens { tokens[i] = fmt.Sprintf("%.4g", xs[i]) }
	return fmt.Sprintf("[%s]", strings.Join(tokens, ","))
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

func printShellStats(shell analyze.Shell, ID, cIdx, samples int) {
	c := colors[cIdx % len(colors)]
	v := shell.Volume(samples)
	Ix, Iy, Iz := shell.Moments(samples)
	sa := shell.SurfaceArea(samples)
	fmt.Printf(
		"%5d) Vol: %7.4g SA: %7.4g Is: (%7.4g %7.4g %7.4g) (%s)\n",
		ID, v, sa, Ix, Iy, Iz, c,
	)
}
