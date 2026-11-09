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
	pp := &s.P[p]
	old := pp.RunNext
	pp.RunNext = g
	if old == "" {
		return
	}
	if len(pp.Local) < s.LocalCap {
		pp.Local = append(pp.Local, old)
		return
	}
	// Очередь полна: половина с головы и вытесненная горутина
	// уходят в глобальную очередь (runqputslow).
	half := s.LocalCap / 2
	s.Global = append(s.Global, pp.Local[:half]...)
	s.Global = append(s.Global, old)
	pp.Local = append([]string(nil), pp.Local[half:]...)
}

// Gosched — горутина g уступила процессор и встаёт в хвост глобальной очереди.
func (s *Sched) Gosched(g string) {
	s.Global = append(s.Global, g)
}

// FindRunnable — один шаг планировщика на процессоре p:
// какую горутину он запустит. ok == false — работы нет нигде.
func (s *Sched) FindRunnable(p int) (g string, ok bool) {
	g, ok = s.find(p)
	if ok {
		s.P[p].Tick++
	}
	return g, ok
}

func (s *Sched) find(p int) (string, bool) {
	pp := &s.P[p]

	// 1. Справедливость: раз в GlobalEvery запусков — сначала глобальная очередь,
	// иначе две горутины, порождающие друг друга, заняли бы P навсегда.
	if pp.Tick%s.GlobalEvery == 0 && len(s.Global) > 0 {
		g := s.Global[0]
		s.Global = s.Global[1:]
		return g, true
	}

	// 2. runnext.
	if pp.RunNext != "" {
		g := pp.RunNext
		pp.RunNext = ""
		return g, true
	}

	// 3. Локальная очередь.
	if len(pp.Local) > 0 {
		g := pp.Local[0]
		pp.Local = pp.Local[1:]
		return g, true
	}

	// 4. Глобальная очередь — пачкой: первая запускается, остальные — к себе.
	if len(s.Global) > 0 {
		n := min(len(s.Global), len(s.Global)/len(s.P)+1, s.LocalCap/2)
		batch := s.Global[:n]
		s.Global = s.Global[n:]
		pp.Local = append(pp.Local, batch[1:]...)
		return batch[0], true
	}

	// 5. Кража: половина (с округлением вверх) с головы очереди соседа.
	// Последняя украденная запускается, остальные — в свою очередь.
	for i := 1; i < len(s.P); i++ {
		v := &s.P[(p+i)%len(s.P)]
		if len(v.Local) == 0 {
			continue
		}
		k := len(v.Local) - len(v.Local)/2
		stolen := v.Local[:k]
		v.Local = append([]string(nil), v.Local[k:]...)
		pp.Local = append(pp.Local, stolen[:k-1]...)
		return stolen[k-1], true
	}

	// 6. Последний шанс — чужой runnext.
	for i := 1; i < len(s.P); i++ {
		v := &s.P[(p+i)%len(s.P)]
		if v.RunNext != "" {
			g := v.RunNext
			v.RunNext = ""
			return g, true
		}
	}
	return "", false
}
