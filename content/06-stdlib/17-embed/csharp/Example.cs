// В .csproj (RootNamespace = MyApp):
//   <ItemGroup>
//     <EmbeddedResource Include="Data\*.txt" />   <!-- или поштучно: Data\greeting.txt -->
//   </ItemGroup>
using System.Reflection;

var asm = Assembly.GetExecutingAssembly();

// Имя ресурса склеено из корневого пространства имён и пути через точки.
// Опечатка обнаружится только в рантайме: поток будет null.
// В Go отсутствующий файл в //go:embed — ошибка компиляции.
using (var stream = asm.GetManifestResourceStream("MyApp.Data.greeting.txt")
                    ?? throw new FileNotFoundException("ресурс не найден"))
using (var reader = new StreamReader(stream))
{
    Console.Write(reader.ReadToEnd()); // в Go — готовая строка, без чтения потока
}

// Аналог embed.FS + fs.ReadDir: перечисление имён ресурсов — плоский список строк,
// без каталогов. В Go embed.FS сохраняет структуру каталогов и реализует fs.FS.
foreach (var name in asm.GetManifestResourceNames().Where(n => n.EndsWith(".txt")).Order())
{
    Console.WriteLine(name);
}
