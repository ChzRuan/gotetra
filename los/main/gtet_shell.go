package main

import (
	"flag"
	"log"
	"math"
	"runtime"
	"sort"
	
	"github.com/phil-mansfield/gotetra/los"
	"github.com/phil-mansfield/gotetra/los/geom"
	rgeom "github.com/phil-mansfield/gotetra/render/geom"
	"github.com/phil-mansfield/gotetra/los/analyze"
	util "github.com/phil-mansfield/gotetra/los/main/gtet_util"
	"github.com/phil-mansfield/gotetra/render/io"
	"github.com/phil-mansfield/gotetra/render/halo"
	"github.com/phil-mansfield/gotetra/math/rand"
	intr "github.com/phil-mansfield/gotetra/math/interpolate"
)

type Params struct {
	// HaloProfiles params
	RBins, Spokes, Rings int
	MaxMult, MinMult float64

	// Splashback params
	HFactor float64
	Order, Window, Levels, SubsampleLength int
	Cutoff float64

	// Alternate modes
	MedianProfile, MeanProfile, SphericalProfile bool
	SphericalProfilePoints int
	SphericalProfileTriLinearPoints int
	SphericalProfileTriCubicPoints int
}

func main() {
	// Parse.
	log.Println("gtet_shell")
	p := parseCmd()
	ids, snaps, _, err := util.ParseStdin()
	if err != nil { log.Fatal(err.Error()) }

	if len(ids) == 0 { log.Fatal("No input IDs.") }

	// We're just going to do this part separately.
	if p.SphericalProfile {
		out, err := profile(ids, snaps, p)
		if err != nil { log.Fatal(err.Error()) }
		util.PrintRows(ids, snaps, out)
		return
	}
	
	// Compute coefficients.
	out := make([][]float64, len(ids))
	snapBins, idxBins := binBySnap(snaps, ids)
	buf := make([]analyze.RingBuffer, p.Rings)
	for i := range buf { buf[i].Init(p.Spokes, p.RBins) }

	sortedSnaps := []int{}
	for snap := range snapBins {
		sortedSnaps = append(sortedSnaps, snap)
	}
	sort.Ints(sortedSnaps)

	var losBuf *los.Buffers

	var rowLength int
	switch {
	case p.MedianProfile:
		rowLength = p.RBins * 2
	case p.MeanProfile:
		rowLength = p.RBins * 2
	default:
		rowLength = p.Order*p.Order*2
	}
	
	for i := range out {
		out[i] = make([]float64, rowLength)
	}

	
MainLoop:
	for _, snap := range sortedSnaps { 
		if snap == -1 { continue }
		snapIDs := snapBins[snap]
		idxs := idxBins[snap]

		// Bin halos
		hds, files, err := util.ReadHeaders(snap)
		
		if err != nil { log.Fatal(err.Error()) }
		if losBuf == nil {
			losBuf = los.NewBuffers(files[0], &hds[0], p.SubsampleLength)
		}
		halos, err := createHalos(snap, &hds[0], snapIDs, p)
		for i := range halos {
			// Screw it, we're too early in the catalog. Abort!
			if !halos[i].IsValid { continue MainLoop }
		}

		ms := runtime.MemStats{}
		runtime.ReadMemStats(&ms)
		log.Printf(
			"gtet_shell: Alloc: %d MB, Sys: %d MB",
			ms.Alloc / 1000000, ms.Sys / 1000000,
		)
		
		if err != nil { log.Fatal(err.Error()) }
		intrBins := binIntersections(hds, halos)
		
		// Add densities. Done header by header to limit I/O time.
		hdContainer := make([]io.SheetHeader, 1)
		fileContainer := make([]string, 1)
		for i := range hds {
			runtime.GC()

			if len(intrBins[i]) == 0 { continue }
			hdContainer[0] = hds[i]
			fileContainer[0] = files[i]
			los.LoadPtrDensities(
				intrBins[i], hdContainer, fileContainer, losBuf,
			)
		}
		
		if p.MedianProfile {
			// Calculate median profile.
			for i := range halos {
				runtime.GC()
				out[idxs[i]] = calcMedian(&halos[i], p)
			}
		} else if p.MeanProfile {
			for i := range halos {
				runtime.GC()
				out[idxs[i]] = calcMean(&halos[i], p)
			}
		} else {
			// Calculate Penna coefficients.
			for i := range halos {
				runtime.GC()
				var ok bool
				out[idxs[i]], ok = calcCoeffs(&halos[i], buf, p)
				if !ok { log.Fatal("Welp, fix this.") }
			}
		}
		
	}
	
	util.PrintRows(ids, snaps, out)
}

func parseCmd() *Params {
	// Parse command line.
	p := &Params{}
	flag.IntVar(&p.RBins, "RBins", 256,
		"Number of radial bins used per LoS.")
	flag.IntVar(&p.Spokes, "Spokes", 1024,
		"Number of LoS's used per ring.")
	flag.IntVar(&p.Rings, "Rings", 10,
		"Number of rings used per halo. 3, 4, 6, and 10 rings are\n" + 
			"guaranteed to be uniformly spaced.")
	flag.Float64Var(&p.MaxMult, "MaxMult", 3,
		"Ending radius of LoSs as a multiple of R_200m.")
	flag.Float64Var(&p.MinMult, "MinMult", 0.5,
		"Starting radius of LoSs as a multiple of R_200m.")
	flag.Float64Var(&p.HFactor, "HFactor", 10.0,
		"Factor controling how much an angular wedge can vary from " +
			"its neighbor. (If you don't know what this is, don't change it.)")
	flag.IntVar(&p.Order, "Order", 5,
		"Order of the shell fitting function.")
	flag.IntVar(&p.Window, "Window", 121,
		"Number of bins within smoothign window. Must be odd.")
	flag.IntVar(&p.Levels, "Levels", 4,
		"The number of recurve max-finding levels used by the 2D edge finder.")
	flag.IntVar(&p.SubsampleLength, "SubsampleLength", 1,
		"The number of particle edges per tetrahedron edge. Must be 2^n.")
	flag.Float64Var(&p.Cutoff, "Cutoff", 0.0,
		"The shallowest slope that can be considered a splashback point.")
	flag.BoolVar(&p.MedianProfile, "MedianProfile", false,
		"Compute the median halo profile instead of the shell. " + 
			"KILL THIS OPTION.")
	flag.BoolVar(&p.MeanProfile, "MeanProfile", false,
		"Compute the mean halo profile instead of the shell. " + 
			"KILL THIS OPTION.")
	flag.BoolVar(&p.SphericalProfile, "SphericalProfile", false,
		"Compute the radial profile of a halo using standard particle " +
			"binning. KILL THIS OPTION.")
	flag.IntVar(&p.SphericalProfilePoints, "SphericalProfilePoints", 0,
		"Number of pointers per tetrhedra to use when computing spherical " +
			"If 0, tetrahedra won't be used and the profiles will just be" +
			"computed from the particles.")
	flag.IntVar(&p.SphericalProfileTriLinearPoints,
		"SphericalProfileTriLinearPoints", 0,
		"Number of particles per side of each cube when using tri-linear " +
			"interpolation. If 0, tri-linear interpolation won't be used.")
		flag.IntVar(&p.SphericalProfileTriCubicPoints,
		"SphericalProfileTriCubicPoints", 0,
		"Number of particles per side of each cube when using tri-cubic " +
			"interpolation. If 0, tri-cubic interpolation won't be used.")
	flag.Parse()
	return p
}

func createHalos(
	snap int, hd *io.SheetHeader, ids []int, p *Params,
) ([]los.HaloProfiles, error) {
	vals, err := util.ReadRockstar(
		snap, ids, halo.X, halo.Y, halo.Z, halo.Rad200b,
	)
	if err != nil { return nil, err }

	xs, ys, zs, rs := vals[0], vals[1], vals[2], vals[3]
	g := rand.NewTimeSeed(rand.Xorshift)

	// Initialize halos.
	halos := make([]los.HaloProfiles, len(ids))
	seenIDs := make(map[int]bool)
	for i, id := range ids {
		origin := &geom.Vec{
			float32(xs[i]), float32(ys[i]), float32(zs[i]),
		}

		if rs[i] <= 0 { continue }
		
		// If we've already seen a halo once, randomize its orientation.
		if seenIDs[id] {
			halos[i].Init(
				id, p.Rings, origin, rs[i] * p.MinMult, rs[i] * p.MaxMult,
				p.RBins, p.Spokes, hd.TotalWidth, los.Log(true),
				los.Rotate(float32(g.Uniform(0, 2 * math.Pi)),
                    float32(g.Uniform(0, 2 * math.Pi)),
                    float32(g.Uniform(0, 2 * math.Pi))),
			)
		} else {
			seenIDs[id] = true
			halos[i].Init(
				id, p.Rings, origin, rs[i] * p.MinMult, rs[i] * p.MaxMult,
				p.RBins, p.Spokes, hd.TotalWidth, los.Log(true),
			)
		}
	}

	return halos, nil
}

type profileRange struct {
	rMin, rMax float64
	v0 rgeom.Vec
}

func newProfileRanges(ids, snaps []int, p *Params) ([]profileRange, error) {
	snapBins, idxBins := binBySnap(snaps, ids)
	ranges := make([]profileRange, len(ids))
	for _, snap := range snaps {
		snapIDs := snapBins[snap]
		idxs := idxBins[snap]

		vals, err := util.ReadRockstar(
			snap, snapIDs, halo.X, halo.Y, halo.Z, halo.Rad200b,
		)
		if err != nil { return nil, err }
		xs, ys, zs, rs := vals[0], vals[1], vals[2], vals[3]
		for i := range xs {
			pr := profileRange{
				p.MinMult * rs[i], p.MaxMult * rs[i],
				rgeom.Vec{ float32(xs[i]), float32(ys[i]), float32(zs[i]) },
			}
			ranges[idxs[i]] = pr
		}
	}
	return ranges, nil
}

func profile(ids, snaps []int, p *Params) ([][]float64, error) {
	// Normal Set up
	ranges, err := newProfileRanges(ids, snaps, p)
	if err != nil { return nil, err }
	
	// tetra and tri-linear setup.
	var (
		xs []rgeom.Vec
		vecBuf []rgeom.Vec
		randBuf []float64
		gen *rand.Generator

		profs []*sphericalProfile
		intrBuf *intrBuffers
		con intrConstructor
		triPts int
	)

	if p.SphericalProfilePoints > 0 {
		gen = rand.NewTimeSeed(rand.Xorshift)
		vecBuf = make([]rgeom.Vec, p.SphericalProfilePoints)
		randBuf = make([]float64, 3 * p.SphericalProfilePoints)
	} else if p.SphericalProfileTriLinearPoints > 0 {
		triPts = p.SphericalProfileTriLinearPoints
		con = func(x0, dx float64, nx int,
			y0, dy float64, ny int, 
			z0, dz float64, nz int, vals []float64) intr.TriInterpolator {
				return intr.NewUniformTriLinear(
					x0, dx, nx, y0, dy, ny, z0, dz, nz, vals,
				)
		}
	} else if p.SphericalProfileTriCubicPoints > 0 {
		triPts = p.SphericalProfileTriCubicPoints
		con = func(x0, dx float64, nx int,
			y0, dy float64, ny int, 
			z0, dz float64, nz int, vals []float64) intr.TriInterpolator {
				return intr.NewUniformTriCubic(
					x0, dx, nx, y0, dy, ny, z0, dz, nz, vals,
				)
		}
	}


	// Bin particles
	snapBins, idxBins := binBySnap(snaps, ids)
	for snap := range snapBins {
		runtime.GC()
		
		idxs := idxBins[snap]
		hds, files, err := util.ReadHeaders(snap)
		if err != nil { return nil, err }
		if len(xs) == 0 {
			n := hds[0].GridWidth*hds[0].GridWidth*hds[0].GridWidth
			xs = make([]rgeom.Vec, n)

			if p.SphericalProfile {
				profs = make([]*sphericalProfile, len(ranges))
				for i, r := range ranges {
					profs[i] = newSphericalProfile(r.v0, r.rMin, r.rMax,
						hds[0].TotalWidth, hds[0].CountWidth, p.RBins)
				}
			}

			if triPts > 0 {
				intrBuf = newIntrBuffers(
					int(hds[0].SegmentWidth), 
					int(hds[0].GridWidth), p.SubsampleLength,
				)
			}
		}
		
		intrBins := binRangeIntersections(hds, ranges, idxs)
		for i := range hds {
			if len(intrBins[i]) == 0 { continue }
			log.Printf("%d%d%d -> (%d)", i / 64, (i / 8) % 8, i % 8,
				len(intrBins[i]))
			err := io.ReadSheetPositionsAt(files[i], xs)
			if err != nil { return nil, err }
			for _, j := range intrBins[i] {
				if p.SphericalProfilePoints > 0 {
					tetraBinParticles(
						&hds[i], xs, p.SubsampleLength, profs[idxs[j]],
						vecBuf, randBuf, gen,
					)
				} else if triPts > 0 {
					interpolatorBinParticles(
						xs, triPts, profs[idxs[j]], con, intrBuf,
					)
				} else {
					binParticles(&hds[i], xs, p.SubsampleLength, profs[idxs[j]])
				}
			}
		}
	}
	
	// Convert
	for i := range profs {
		if err != nil { return nil, err }
		countsToRhos(
			profs[i], p.SubsampleLength, p.SphericalProfilePoints, triPts,
		)
	}

	counts := make([][]float64, len(profs))
	for i := range counts { counts[i] = profs[i].counts }
	
	outs := prependRadii(counts, ranges)
	
	return outs, nil
}

func countsToRhos(prof *sphericalProfile, skip, tetraPoints, triPoints int) {
	dx :=prof.boxWidth / prof.countWidth
	mp := dx*dx*dx

	if tetraPoints > 0 {
		mp *= float64(skip*skip*skip) / float64(6*tetraPoints)
	} else if triPoints > 0 {
		mp *= float64(skip*skip*skip) / float64(triPoints*triPoints*triPoints)
	}

	lrMin, lrMax := math.Log(prof.rMin), math.Log(prof.rMax)
	dlr := (lrMax - lrMin) / float64(len(prof.counts))
	for i := range prof.counts {
		rLo := math.Exp(dlr*float64(i) + lrMin)
		rHi := math.Exp(dlr*float64(i + 1) + lrMin)
		dV := (rHi*rHi*rHi - rLo*rLo*rLo) * 4 * math.Pi / 3
		prof.counts[i] *= mp / dV
	}
}

func tetraBinParticles(
	hd *io.SheetHeader, xs []rgeom.Vec, skip int, prof *sphericalProfile,
	vecBuf []rgeom.Vec, randBuf []float64, gen *rand.Generator,
) {
	sw, gw := int(hd.SegmentWidth), int(hd.GridWidth)
	for iz := 0; iz < sw; iz += skip {
		for iy := 0; iy < sw; iy += skip {
			for ix := 0; ix < sw; ix += skip {
				idx := ix + gw*iy + gw*gw*iz
				for dir := 0; dir < 6; dir++ {
					tetraPoints(idx, dir, gw, skip, xs, gen, randBuf, vecBuf)
					for _, pt := range vecBuf {
						x := float64(pt[0])
						y := float64(pt[1])
						z := float64(pt[2])
						prof.insert(x, y, z)
					}
				}
			}
		}
	}
}

////////////////////////////////////
// Interpolation Helper Functions //
////////////////////////////////////

// intrConstructor creates a new 3D interpolator.
type intrConstructor func(
	float64, float64, int,
	float64, float64, int,
	float64, float64, int, []float64) intr.TriInterpolator

// intrBuffers contains the space required for interpolating.
type intrBuffers struct {
	gw, sw, kw, skip int
	xs, ys, zs []float64
	vecIntr, boxIntr []bool
}

// newIntrBuffers allocates a new set of buffers for a given set of grid
// parameters.
func newIntrBuffers(segWidth, gridWidth, skip int) *intrBuffers {
	buf := &intrBuffers{}
	buf.gw = gridWidth // Remember! This is the bigger one!!!
	buf.sw = segWidth
	buf.skip = skip
	buf.kw = (segWidth/skip) + 1

	buf.xs = make([]float64, buf.kw*buf.kw*buf.kw)
	buf.ys = make([]float64, len(buf.xs))
	buf.zs = make([]float64, len(buf.xs))
	
	buf.vecIntr = make([]bool, buf.kw*buf.kw*buf.kw)
	buf.boxIntr = make([]bool, (buf.kw-1)*(buf.kw-1)*(buf.kw-1))

	return buf
}

// loadBuffers inserts a set of vectors into the intrBuffers. The points are
// assumed to have undergone a coordinate transformation such that they
// are contiguous (read: call sphericalProfile.transform() on them first).
func (buf *intrBuffers) loadVecs(vecs []rgeom.Vec, sp *sphericalProfile) {
	kw, gw, s := buf.kw, buf.gw, buf.skip

	// Construct per-vector buffers.
	ik := 0
	for zk := 0; zk < kw; zk++ {
		for yk := 0; yk < kw; yk++ {
			for xk := 0; xk < kw; xk++ {
				xg, yg, zg := xk*s, yk*s, zk*s
				ig := xg + yg*gw + zg*gw*gw
				x := float64(vecs[ig][0])
				y := float64(vecs[ig][1])
				z := float64(vecs[ig][2])
				buf.vecIntr[ik] = sp.contains(x, y, z)

				buf.xs[ik] = x
				buf.ys[ik] = y
				buf.zs[ik] = z

				ik++
			}
		}
	}
	
	// Construct per-box buffers.
	v := buf.vecIntr
	i := 0
	for z0 := 0; z0 < kw-1; z0++ {
		z1 := z0 + 1
		for y0 := 0; y0 < kw-1; y0++ {
			y1 := y0 + 1
			for x0 := 0; x0 < kw-1; x0++ {
				x1 := x0 + 1
				
				i000 := x0 + y0*kw + z0*kw*kw
				i001 := x0 + y0*kw + z1*kw*kw
				i010 := x0 + y1*kw + z0*kw*kw
				i011 := x0 + y1*kw + z1*kw*kw

				i100 := x1 + y0*kw + z0*kw*kw
				i101 := x1 + y0*kw + z1*kw*kw
				i110 := x1 + y1*kw + z0*kw*kw
				i111 := x1 + y1*kw + z1*kw*kw
				
				buf.boxIntr[i] = v[i000] || v[i001] || v[i010] || v[i011] ||
					v[i100] || v[i101] || v[i110] || v[i111]
				
				i++
			}
		}
	}
}

// Used for load balancing.
func (buf *intrBuffers) zCounts() []int {
	counts := make([]int, buf.kw-1)

	i := 0
	for z := 0; z < buf.kw-1; z++ {
		for y := 0; y < buf.kw-1; y++ {
			for x := 0; x < buf.kw-1; x++ {
				if buf.boxIntr[i] { counts[z]++ }
				i++
			}
		}
	}
	
	return counts
}

// Used for load balanacing.
func zSplit(zCounts []int, workers int) [][]int {
	tot := 0
	for _, n := range zCounts { tot += n }

	splits := make([]int, workers + 1)
	si := 1
	splitWidth := tot / workers
	if splitWidth * workers < tot { splitWidth++ }
	target := splitWidth

	sum := 0
	for i, n := range zCounts {
		sum += n
		if sum > target {
			splits[si] = i
			for sum > target { target += splitWidth }
			si++
		}
	}
	for ; si < len(splits); si++ { splits[si] = len(zCounts) }

	splitIdxs := make([][]int, workers)
	for i := range splitIdxs {
		jStart, jEnd := splits[i], splits[i + 1]
		for j := jStart; j < jEnd; j++ {
			if zCounts[j] > 0 { splitIdxs[i] = append(splitIdxs[i], j) }
		}
	}

	return splitIdxs
}

// sphericalProfile represents a particle-counting profile for a halo.
type sphericalProfile struct {
	origin [3]float64
	counts []float64
	boxWidth, countWidth float64
	rMin, rMax, rMin2, rMax2 float64
	lrMin, lrMax, dlr float64
}

// newSphericalProfile does what you think it does.
func newSphericalProfile(
	origin rgeom.Vec, rMin, rMax, boxWidth float64, countWidth int64, rBins int,
) *sphericalProfile {
	sp := &sphericalProfile{}
	
	sp.origin[0] = float64(origin[0])
	sp.origin[1] = float64(origin[1])
	sp.origin[2] = float64(origin[2])

	sp.rMin = rMin
	sp.rMax = rMax
	sp.rMin2 = rMin*rMin
	sp.rMax2 = rMax*rMax
	sp.lrMax = math.Log(rMax)
	sp.lrMin = math.Log(rMin)
	sp.dlr = (sp.lrMax - sp.lrMin) / float64(rBins)
	sp.counts = make([]float64, rBins)
	sp.boxWidth = boxWidth
	sp.countWidth = float64(countWidth)

	return sp
}

// transform does a coordinate transformation on the given vectors so that 
// they are as close to the given halo as possible.
func (sp *sphericalProfile) transform(vecs []rgeom.Vec) {
	x0 := float32(sp.origin[0])
	y0 := float32(sp.origin[1])
	z0 := float32(sp.origin[2])
	tw := float32(sp.boxWidth)
	tw2 := tw / 2

	for i, vec := range vecs {
		x, y, z := vec[0], vec[1], vec[2]
		dx, dy, dz := x - x0, y - y0, z - z0

		if dx > tw2 {
			vecs[i][0] -= tw
		} else if dx < -tw2 {
			vecs[i][0] += tw
		}

		if dy > tw2 {
			vecs[i][1] -= tw
		} else if dy < -tw2 {
			vecs[i][1] += tw
		}

		if dz > tw2 {
			vecs[i][2] -= tw
		} else if dz < -tw2 {
			vecs[i][2] += tw
		}
	}
}

// r2 returns the square of the distance between the givne point and the
// center of the profile.
func (sp *sphericalProfile) r2(x, y, z float64) float64 {
	x0, y0, z0 := sp.origin[0], sp.origin[1], sp.origin[2]
	dx, dy, dz := x - x0, y - y0, z - z0
	return dx*dx + dy*dy + dz*dz
}

// contains returns true if the given point is inside the profile
func (sp *sphericalProfile) contains(x, y, z float64) bool {
	r2 := sp.r2(x, y, z)
	return sp.rMin2 < r2 && r2 < sp.rMax2
}

// insert inserts the given point into the profile with the given weight 
// if possible. If the point is inserted true is returned, otherwise false is
// returned.
func (sp *sphericalProfile) insert(x, y, z float64) bool {
	r2 := sp.r2(x, y, z)
	if r2 <= sp.rMin2 || r2 >= sp.rMax2 { return false }
	lr := math.Log(r2) / 2
	ir := int((lr - sp.lrMin) / sp.dlr)
	sp.counts[ir]++

	return true
}

var hits = 0
var passes = 0

// interpolatorBinParticles places the density field represented by the given
// points into the given profile.
func interpolatorBinParticles(
	vecs []rgeom.Vec, pts int, prof *sphericalProfile,
	con intrConstructor, buf *intrBuffers,
) {
	prof.transform(vecs)
	buf.loadVecs(vecs, prof)

	// Yup... lots of allocations happening here... -___-
	// This could be improved.
	runtime.GC()

	triX := con(0, 1, buf.kw, 0, 1, buf.kw, 0, 1, buf.kw, buf.xs)
	triY := con(0, 1, buf.kw, 0, 1, buf.kw, 0, 1, buf.kw, buf.ys)
	triZ := con(0, 1, buf.kw, 0, 1, buf.kw, 0, 1, buf.kw, buf.zs)
	
	xBuf := make([]int, 0, buf.kw*buf.kw)
	yBuf := make([]int, 0, buf.kw*buf.kw)
	
	i := 0
	for z := 0; z < buf.kw-1; z++ {
		xBuf := xBuf[0:0]
		yBuf := yBuf[0:0]
		for y := 0; y < buf.kw-1; y++ {
			for x := 0; x < buf.kw-1; x++ {
				if buf.boxIntr[i] {
					xBuf = append(xBuf, x)
					yBuf = append(yBuf, y)
				}
				i++
			}
		}
		
		if len(xBuf) > 0 {
			xyInterpolate(xBuf, yBuf, z, triX, triY, triZ, pts, prof)
		}
	}
}

func xyInterpolate(
	xBuf, yBuf []int, zIdx int,
	triX, triY, triZ intr.TriInterpolator,
	pts int, prof *sphericalProfile,
) {
	dp := 1 / float64(pts)
	z0 := float64(zIdx)

	// xl, yl, zl - Lagrangian values in code units.
	
	for zi := 0; zi < pts; zi++ {
		zl := z0 + float64(zi) * dp

		// Iterate over y indices.
		iStart, iEnd := 0, 0
		for iEnd < len(xBuf) {
			yIdx := yBuf[iStart]
			y0 := float64(yIdx)

			// Find the index range of the current yIdx.
			for iEnd = iStart; iEnd < len(xBuf); iEnd++ {
				if yBuf[iEnd] != yIdx { break }
			}

			for yi := 0; yi < pts; yi++ {
				yl := y0 + float64(yi) * dp
				// Iterate over x indices.
				for _, xIdx := range xBuf[iStart: iEnd] {
					x0 := float64(xIdx)
					for xi := 0; xi < pts; xi++ {
						xl := x0 + float64(xi) * dp

						x := triX.Eval(xl, yl, zl)
						y := triY.Eval(xl, yl, zl)
						z := triZ.Eval(xl, yl, zl)
						
						prof.insert(x, y, z)
					}
				}
			}

			iStart = iEnd
		}
	}
}

func tetraPoints(
	idx, dir, gw, skip int, xs []rgeom.Vec,
	gen *rand.Generator, randBuf []float64, vecBuf []rgeom.Vec,
) {
	idxBuf, tet := &rgeom.TetraIdxs{}, &rgeom.Tetra{}
	idxBuf.Init(int64(idx), int64(gw), int64(skip), dir)
	i0, i1, i2, i3 := idxBuf[0], idxBuf[1], idxBuf[2], idxBuf[3]
	tet.Init(&xs[i0], &xs[i1], &xs[i2], &xs[i3])
	tet.RandomSample(gen, randBuf, vecBuf)
}

func binParticles(
	hd *io.SheetHeader, xs []rgeom.Vec, skip int, prof *sphericalProfile,
) {
	prof.transform(xs)
	sw, gw := int(hd.SegmentWidth), int(hd.GridWidth)
	for iz := 0; iz < sw; iz += skip {
		for iy := 0; iy < sw; iy += skip {
			for ix := 0; ix < sw; ix += skip {
				pt := xs[ix + iy*gw + iz*gw*gw]
				x, y, z := float64(pt[0]), float64(pt[1]), float64(pt[2])
				prof.insert(x, y, z)
			}
		}
	}
}

func binIntersections(
	hds []io.SheetHeader, halos []los.HaloProfiles,
) [][]*los.HaloProfiles {

	bins := make([][]*los.HaloProfiles, len(hds))
	for i := range hds {
		for hi := range halos {
			if (&halos[hi]).SheetIntersect(&hds[i]) {
				bins[i] = append(bins[i], &halos[hi])
			}
		}
	}
	return bins
}

func binRangeIntersections(
	hds []io.SheetHeader, ranges []profileRange, idxs []int,
) [][]int {
	bins := make([][]int, len(hds))
	for i := range hds {
		for _, j := range idxs {
			if sheetIntersect(ranges[idxs[j]], &hds[i]) {
				bins[i] = append(bins[i], j)
			}
		}
	}
	return bins
}

func sheetIntersect(r profileRange, hd *io.SheetHeader) bool {
	tw := float32(hd.TotalWidth)
	return inRange(r.v0[0], float32(r.rMax), hd.Origin[0], hd.Width[0], tw) &&
		inRange(r.v0[1], float32(r.rMax), hd.Origin[1], hd.Width[1], tw) &&
		inRange(r.v0[2], float32(r.rMax), hd.Origin[2], hd.Width[2], tw)
}

func inRange(x, r, low, width, tw float32) bool {
	return wrapDist(x, low, tw) > -r && wrapDist(x, low + width, tw) < r
}

func wrapDist(x1, x2, width float32) float32 {
	dist := x1 - x2
	if dist > width / 2 {
		return dist - width
	} else if dist < width / -2 {
		return dist + width
	} else {
		return dist
	}
}

func prependRadii(rhos [][]float64, ranges []profileRange) [][]float64 {
	out := make([][]float64, len(rhos))
	for i := range rhos {
		rs := make([]float64, len(rhos[i]))
		lrMin, lrMax := math.Log(ranges[i].rMin), math.Log(ranges[i].rMax)
		dlr := (lrMax - lrMin) / float64(len(rhos[i]))
		for j := range rs {
			rs[j] = math.Exp((float64(j) + 0.5) * dlr + lrMin)
		}
		out[i] = append(rs, rhos[i]...)
	}
	return out
}

func calcCoeffs(
	halo *los.HaloProfiles, buf []analyze.RingBuffer, p *Params,
) ([]float64, bool) {
	for i := range buf {
		buf[i].Clear()
		buf[i].Splashback(halo, i, p.Window, p.Cutoff)
	}
	pxs, pys, ok := analyze.FilterPoints(buf, p.Levels, p.HFactor)
	if !ok { return nil, false }
	cs, _ := analyze.PennaVolumeFit(pxs, pys, halo, p.Order, p.Order)
	return cs, true
}

func calcMedian(halo *los.HaloProfiles, p *Params) []float64 {
	rs := make([]float64, p.RBins)
	halo.GetRs(rs)
	rhos := halo.MedianProfile()
	return append(rs, rhos...)
}

func calcMean(halo *los.HaloProfiles, p *Params) []float64 {
	rs := make([]float64, p.RBins)
	halo.GetRs(rs)
	rhos := halo.MeanProfile()
	return append(rs, rhos...)
}

func binBySnap(snaps, ids []int) (snapBins, idxBins map[int][]int) {
	snapBins = make(map[int][]int)
	idxBins = make(map[int][]int)
	for i, snap := range snaps {
		id := ids[i]
		snapBins[snap] = append(snapBins[snap], id)
		idxBins[snap] = append(idxBins[snap], i)
	}
	return snapBins, idxBins
}
