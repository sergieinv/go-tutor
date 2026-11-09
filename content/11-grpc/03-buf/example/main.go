// buf работает не с текстом `.proto`, а с дескрипторами:
// компилирует файлы в набор `FileDescriptorProto` (в терминах
// buf — образ, image) и прогоняет по нему правила lint и
// breaking. Тот же дескриптор вкомпилирован в сгенерированный
// код — переменная `File_orders_proto`. Пример обходит его
// так, как это делает линтер, и повторяет одно правило buf.

package main

import (
	"fmt"
	"log"
	"strings"
	"unicode"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func main() {
	fd := File_orders_proto

	// Файл: путь внутри модуля, пакет и импорты. Импорт
	// `google/protobuf/timestamp.proto` — часть protobuf, а
	// googleapis и protovalidate buf берёт из `deps` в
	// `buf.yaml` по коммитам из `buf.lock`.
	fmt.Println("файл:", fd.Path(), "пакет:", fd.Package())
	for i := range fd.Imports().Len() {
		fmt.Println("  импорт:", fd.Imports().Get(i).Path())
	}

	// Сервисы и методы. Полное имя метода — то, что gRPC
	// отправит в заголовке `:path`; по нему же настраивают
	// interceptors и правила доступа.
	for i := range fd.Services().Len() {
		sd := fd.Services().Get(i)
		for j := range sd.Methods().Len() {
			md := sd.Methods().Get(j)
			fmt.Printf("метод /%s/%s(%s) -> %s\n", sd.FullName(), md.Name(),
				md.Input().Name(), md.Output().Name())
		}
	}

	// Сообщения и поля: номер, имя, тип и JSON-имя. Имя в
	// lowerCamelCase не мешает компиляции, но JSON-имя у
	// `customerEmail` и `customer_email` одно и то же — вот
	// почему стиль имён проверяют машиной.
	for i := range fd.Messages().Len() {
		m := fd.Messages().Get(i)
		fmt.Println("сообщение", m.Name())
		for j := range m.Fields().Len() {
			f := m.Fields().Get(j)
			fmt.Printf("  %d %s %s json=%s\n", f.Number(), f.Name(), f.Kind(), f.JSONName())
		}
	}

	// Одно правило buf своими руками — `ENUM_VALUE_PREFIX`:
	// значения enum начинаются с имени типа в UPPER_SNAKE_CASE.
	// Значения enum в protobuf живут в области видимости
	// пакета (полное имя — `shop.v1.PAID`, без имени enum), и
	// без префикса `PAID` из двух разных enum столкнутся. Строк
	// и столбцов, как у buf, не будет: в сгенерированный код
	// дескриптор попадает без информации об исходнике.
	for i := range fd.Enums().Len() {
		e := fd.Enums().Get(i)
		prefix := upperSnake(string(e.Name())) + "_"
		for j := range e.Values().Len() {
			v := e.Values().Get(j)
			if !strings.HasPrefix(string(v.Name()), prefix) {
				fmt.Printf("ENUM_VALUE_PREFIX %s: нужен префикс %s\n", v.FullName(), prefix)
			}
		}
	}

	// Образ: дескриптор файла и всех его зависимостей в одном
	// `FileDescriptorSet`. Его buf передаёт плагинам генерации
	// и хранит для `buf breaking` (`buf build -o image.binpb`).
	set := &descriptorpb.FileDescriptorSet{}
	addWithDeps(set, fd, map[string]bool{})
	b, err := proto.Marshal(set)
	if err != nil {
		log.Fatal(err)
	}
	names := make([]string, len(set.File))
	for i, f := range set.File {
		names[i] = f.GetName()
	}
	fmt.Println("образ:", strings.Join(names, ", "), "| байт > 0:", len(b) > 0)
}

// addWithDeps добавляет зависимости раньше файла — так
// устроен FileDescriptorSet, который buf отдаёт плагинам.
func addWithDeps(set *descriptorpb.FileDescriptorSet, fd protoreflect.FileDescriptor, seen map[string]bool) {
	if seen[fd.Path()] {
		return
	}
	seen[fd.Path()] = true
	for i := range fd.Imports().Len() {
		addWithDeps(set, fd.Imports().Get(i).FileDescriptor, seen)
	}
	set.File = append(set.File, protodesc.ToFileDescriptorProto(fd))
}

// upperSnake: OrderStatus → ORDER_STATUS.
func upperSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}
