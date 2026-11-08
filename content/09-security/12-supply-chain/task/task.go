package task

// Vuln — запись локальной базы уязвимостей: версии модуля Module
// в диапазоне [Introduced, Fixed) уязвимы.
type Vuln struct {
	ID         string
	Module     string
	Introduced string // "" — с самой первой версии
	Fixed      string // "" — исправления нет
}

// CompareVersions сравнивает версии модулей Go по правилам semver:
// -1, если a < b; 0, если равны; +1, если a > b.
func CompareVersions(a, b string) int {
	// TODO: реализуйте функцию
	return 0
}

// Audit проверяет go.mod по базе уязвимостей и списку разрешённых replace.
func Audit(gomod string, db []Vuln, allowReplace []string) ([]string, error) {
	// TODO: реализуйте функцию
	return nil, nil
}
