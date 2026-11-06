// Реализация интерфейса объявляется явно в самом типе
Console.WriteLine(Greet(new Dog("rex")));

// Чужой тип без нужного интерфейса придётся оборачивать адаптером,
// даже если у него уже есть метод с подходящей сигнатурой
Console.WriteLine(Greet(new BuilderAdapter(new System.Text.StringBuilder("built"))));

static string Greet(INamer n) => "hello, " + n.Name();

interface INamer
{
    string Name();
}

record Dog(string Nick) : INamer
{
    public string Name() => "dog " + Nick;
}

class BuilderAdapter(System.Text.StringBuilder sb) : INamer
{
    public string Name() => sb.ToString();
}
