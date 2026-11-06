module gotutor/tools/lessonlint

go 1.25

require gopkg.in/yaml.v3 v3.0.1 

require gotutor/runner v0.0.0

// runner живёт в этом же репозитории; go.work подключает его и без replace,
// но replace нужен, чтобы модуль собирался и с GOWORK=off.
replace gotutor/runner => ../../runner
