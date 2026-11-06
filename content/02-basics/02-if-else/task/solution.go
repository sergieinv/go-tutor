package task

import "strconv"

// BMICategory разбирает вес (кг) и рост (м) из строк и возвращает
// категорию индекса массы тела: "underweight", "normal",
// "overweight", "obese" или "invalid" для некорректного ввода.
func BMICategory(weight, height string) string {
	// Значения нужны после проверок, поэтому объявляем их
	// до if, а не в init-выражении.
	w, err := strconv.ParseFloat(weight, 64)
	if err != nil || w <= 0 {
		return "invalid"
	}
	h, err := strconv.ParseFloat(height, 64)
	if err != nil || h <= 0 {
		return "invalid"
	}

	bmi := w / (h * h)
	if bmi < 18.5 {
		return "underweight"
	} else if bmi < 25 {
		return "normal"
	} else if bmi < 30 {
		return "overweight"
	}
	return "obese"
}
