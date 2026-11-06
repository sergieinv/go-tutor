package task

// Point — точка на целочисленной плоскости.
type Point struct{ X, Y int }

// Rect — прямоугольник, заданный двумя углами в любом порядке.
type Rect struct{ Min, Max Point }

// Normalize возвращает прямоугольник, у которого Min.X <= Max.X
// и Min.Y <= Max.Y. Исходное значение не меняется.
func Normalize(r Rect) Rect {
	// r — копия: меняем её свободно, вызывающий не заметит.
	if r.Min.X > r.Max.X {
		r.Min.X, r.Max.X = r.Max.X, r.Min.X
	}
	if r.Min.Y > r.Max.Y {
		r.Min.Y, r.Max.Y = r.Max.Y, r.Min.Y
	}
	return r
}

// Grow расширяет прямоугольник на месте на d по каждой стороне.
// При r == nil ничего не делает.
func Grow(r *Rect, d int) {
	if r == nil {
		return
	}
	r.Min.X -= d
	r.Min.Y -= d
	r.Max.X += d
	r.Max.Y += d
}

// Overlap возвращает пересечение a и b. Если площадь
// пересечения нулевая — Rect{} и false.
func Overlap(a, b Rect) (Rect, bool) {
	a, b = Normalize(a), Normalize(b)
	r := Rect{
		Min: Point{max(a.Min.X, b.Min.X), max(a.Min.Y, b.Min.Y)},
		Max: Point{min(a.Max.X, b.Max.X), min(a.Max.Y, b.Max.Y)},
	}
	if r.Min.X >= r.Max.X || r.Min.Y >= r.Max.Y {
		return Rect{}, false
	}
	return r, true
}
