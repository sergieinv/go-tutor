## Подсказка 1

Начните с `switch st.Status`: `completed`/`compensated` — сразу вернуть; пустой или `running` — цикл вперёд; `compensating`/`failed` — только компенсации; `default` — ошибка с `ErrUnknownStatus`, ничего не вызывая. Компенсации нужны в двух ветках — вынесите их в функцию. Сохранение тоже удобно завернуть: `save := func() error { return store.Save(ctx, sagaID, State{st.Status, slices.Clone(st.Done)}) }`.

## Подсказка 2

Вперёд: `if slices.Contains(st.Done, s.Name) { continue }`, затем `Do`; успех — `st.Status = StatusRunning`, `st.Done = append(st.Done, s.Name)`, `save()`. Ошибка `Do` — запомнить её (`fmt.Errorf("шаг %s: %w", ...)`), выйти из цикла, `st.Status = StatusCompensating`, `save()` и в компенсации. Любая ошибка `save()` — сразу `return "", err`.

## Подсказка 3

Компенсации — цикл, пока `Done` не пуст: берите последний элемент, находите шаг по имени (карта `имя → Step`), вызывайте `Compensate`. Успех — `st.Done = st.Done[:len(st.Done)-1]` и `save()`. Ошибка — `st.Status = StatusFailed`, `save()`, `return StatusFailed, errors.Join(stepErr, compErr)` (`errors.Join` пропускает `nil`). В конце — `StatusCompensated`, `save()` и `return StatusCompensated, stepErr` (в продолженной саге `stepErr == nil`).
