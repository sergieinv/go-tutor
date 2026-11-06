package task

// Point — точка на целочисленной плоскости.
type Point struct{ X, Y int }

// Rect — прямоугольник, заданный двумя углами в любом порядке.
type Rect struct{ Min, Max Point }

// Normalize возвращает прямоугольник, у которого Min.X <= Max.X
// и Min.Y <= Max.Y. Исходное значение не меняется.
func Normalize(r Rect) Rect {
	// TODO: реализуйте функцию
	return Rect{}
}

// Grow расширяет прямоугольник на месте на d по каждой стороне.
// При r == nil ничего не делает.
func Grow(r *Rect, d int) {
	// TODO: реализуйте функцию
}

// Overlap возвращает пересечение a и b. Если площадь
// пересечения нулевая — Rect{} и false.
func Overlap(a, b Rect) (Rect, bool) {
	// TODO: реализуйте функцию
	return Rect{}, false
}
