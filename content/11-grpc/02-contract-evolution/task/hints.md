## Подсказка 1

Два прохода. Первый — по `prev.Fields()` (`Len()` и `Get(i)`): для каждого поля ищите пару `next.Fields().ByNumber(f.Number())`. `nil` — поле удалено, проверьте `next.ReservedRanges().Has(num)`. Второй проход — по `next.Fields()`: `prev.ReservedRanges().Has(num) || prev.ReservedNames().Has(name)`. В конце — `slices.SortFunc` по номеру и правилу (`cmp.Or(cmp.Compare(...), cmp.Compare(...))`).

## Подсказка 2

Вид поля: `f.IsMap()` → map, иначе `f.IsList()` → список, иначе одиночное. Порядок проверок важен: map в protobuf — это тоже `repeated` сообщений, поэтому сначала `IsMap`. Если вид разный — добавьте `FIELD_SAME_CARDINALITY` и `continue`. Тип сравнивайте функцией: для map — рекурсивно `MapKey()` и `MapValue()`, иначе `Kind()`, а для сообщений и enum — ещё `Message().FullName()` и `Enum().FullName()`.

## Подсказка 3

Имя oneof:

```go
func oneofName(f protoreflect.FieldDescriptor) protoreflect.Name {
	od := f.ContainingOneof()
	if od == nil || od.IsSynthetic() { // optional — не oneof
		return ""
	}
	return od.Name()
}
```

Сравнивайте имена: пустое против непустого — вошло в oneof или вышло, два разных непустых — переехало. JSON-имя — `f.JSONName()`: оно учитывает и явный `json_name`.
