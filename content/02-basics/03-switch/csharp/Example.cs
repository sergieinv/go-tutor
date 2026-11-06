string day = "sat";
switch (day)
{
    case "sat":           // подряд идущие метки делят одно тело
    case "sun":
        Console.WriteLine("выходной");
        break;            // break обязателен, иначе ошибка компиляции
    default:
        Console.WriteLine("рабочий");
        break;
}

// Аналог switch без выражения — паттерны с when или switch-выражение
int t = 12;
string weather = t switch
{
    < 0 => "мороз",
    < 20 => "прохладно",
    _ => "тепло",
};

// Явный переход в другую ветку
int level = 2;
switch (level)
{
    case 2: Console.Write("подробно → "); goto case 1;
    case 1: Console.WriteLine("кратко"); break;
}
