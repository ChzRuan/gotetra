/*package density interpolates sequences of particle positions onto a density
grid.
*/
package density

import (
	"github.com/phil-mansfield/gotetra/rand"
	"github.com/phil-mansfield/gotetra/geom"
)

// Interpolator creates a grid-based density distribution from seqeunces of
// positions.
type Interpolator interface {
	// Interpolate adds the density distribution implied by points to the
	// density grid used by the Interpolator. Particles should all be within
	// the bounds of the bounding grid and points not within the interpolation
	// grid will be ignored.
	Interpolate(
		rhos []float64, cb *geom.CellBounds, mass float64, xs []geom.Vec,
		low, high int,
	)
}

type ngp struct { }

// The ordering of these fields makes no goddamned sense.
type mcarlo struct {
	subIntr Interpolator
	segWidth int64
	points int

	gen *rand.Generator

	skip int64
	// Buffers
	idxBuf geom.TetraIdxs
	tet geom.Tetra

	unitBufs [][]geom.Vec
	vecBuf []geom.Vec
}

func NearestGridPoint() Interpolator {
	return &ngp{}
}

func MonteCarlo(
	segWidth int64,
	points int,
	skip int64,
	unitBufs [][]geom.Vec,
) Interpolator {
	mc := &mcarlo{
		NearestGridPoint(), segWidth, points,
		rand.NewTimeSeed(rand.Golang), skip,
		geom.TetraIdxs{}, geom.Tetra{},
		unitBufs, make([]geom.Vec, points),
	}

	return mc
}

// Interpolate interpolates a sequence of particles onto a density grid via a
// nearest grid point scheme.
func (intr *ngp) Interpolate(
	rhos []float64, cb *geom.CellBounds, ptRho float64, xs []geom.Vec,
	low, high int,
) {
	length := cb.Width[0]
	area := cb.Width[0] * cb.Width[1]
	for idx := low; idx < high; idx++ {
		pt := xs[idx]
		i := int(pt[0])
		j := int(pt[1])
		k := int(pt[2])
		rhos[i + j * length + k * area] += ptRho
	}
}

func (intr *mcarlo) Interpolate(
	rhos []float64, cb *geom.CellBounds, ptRho float64, xs []geom.Vec,
	low, high int,
) {
	segWidth := intr.segWidth
	gridWidth := segWidth + 1
	idxWidth := intr.segWidth / intr.skip

	ptRho = ptRho / float64(intr.points) / 6.0 *
		float64(intr.skip * intr.skip * intr.skip)

	for idx := int64(low); idx < int64(high); idx++ {
		x, y, z := coords(idx, idxWidth)
		gridIdx := index(x, y, z, gridWidth)

		for dir := 0; dir < 6; dir++ {
			intr.idxBuf.Init(gridIdx, gridWidth, intr.skip, dir)
					
			intr.tet.Init(
				&xs[intr.idxBuf[0]],
				&xs[intr.idxBuf[1]],
				&xs[intr.idxBuf[2]],
				&xs[intr.idxBuf[3]],
				1e6,
			)
					
			bufIdx := intr.gen.UniformInt(0, len(intr.unitBufs))
			intr.tet.DistributeTetra(
				intr.unitBufs[bufIdx],
				intr.vecBuf,
			)

			intr.subIntr.Interpolate(
				rhos, cb, ptRho, intr.vecBuf, 0, intr.points,
			)
		}
	}
}

func index(x, y, z, cells int64) int64 {
	return x + y * cells + z * cells * cells
}

func coords(idx, cells int64) (x, y, z int64) {
	x = idx % cells
	y = (idx % (cells * cells)) / cells
	z = idx / (cells * cells)
	return x, y, z
}

// AddBuffer adds the contents of a density buffer constrained by the given
// CellBounds to a periodic grid with the given number of cells.
func AddBuffer(grid, buf []float64, cb *geom.CellBounds, cells int) {
	for z := 0; z < cb.Width[2]; z++ {
		zBufIdx := z * cb.Width[0] * cb.Width[1]
		zGridIdx := ((z + cb.Origin[2]) % cells) * cells * cells

		for y := 0; y < cb.Width[1]; y++ {
			yBufIdx := y * cb.Width[0]
			yGridIdx := ((y + cb.Origin[1]) % cells) * cells

			for x := 0; x < cb.Width[0]; x++ {
				xBufIdx := x
				xGridIdx := x + cb.Origin[0]
				if xGridIdx >= cells { xGridIdx -= cells }

				bufIdx := xBufIdx + yBufIdx + zBufIdx
				gridIdx := xGridIdx + yGridIdx + zGridIdx
				
				grid[gridIdx] += float64(buf[bufIdx])
			}
		}
	}
}

func min(x, y int) int {
	if x < y { return x }
	return y
}

func max(x, y int) int {
	if x < y { return y }
	return x
}