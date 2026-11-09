package task

// P — модель процессора планировщика: слот runnext,
// локальная очередь и счётчик запусков.
type P struct {
	RunNext string   // горутина, которая выполнится следующей; "" — слот пуст
	Local   []string // локальная очередь, голова — Local[0]
	Tick    int      // сколько раз FindRunnable на этом P нашла горутину
}

// Sched — модель планировщика: несколько P и общая глобальная очередь.
type Sched struct {
	P           []P
	Global      []string // глобальная очередь, голова — Global[0]
	LocalCap    int      // ёмкость локальной очереди (в рантайме 256)
	GlobalEvery int      // раз в сколько запусков глобальная очередь проверяется первой (в рантайме 61)
}

// Spawn — `go g` на процессоре p: новая горутина занимает runnext,
// прежняя оттуда уходит в хвост локальной очереди.
func (s *Sched) Spawn(p int, g string) {
	// TODO: реализуйте
}

// Gosched — горутина g уступила процессор и встаёт в хвост глобальной очереди.
func (s *Sched) Gosched(g string) {
	// TODO: реализуйте
}

// FindRunnable — один шаг планировщика на процессоре p:
// какую горутину он запустит. ok == false — работы нет нигде.
func (s *Sched) FindRunnable(p int) (g string, ok bool) {
	// TODO: реализуйте
	return "", false
}
