// В .NET встроенного фаззинга нет: есть SharpFuzz (AFL/libFuzzer) и
// property-based-библиотеки FsCheck и CsCheck. С FsCheck свойство выглядит так:
//   Prop.ForAll<string>(s => Rle.Decode(Rle.Encode(s)) == s).QuickCheckThrowOnFailure();
// Ниже — то же без пакетов: случайные входы с фиксированным seed.
using System.Text;

var rnd = new Random(42); // фиксированный seed — прогон воспроизводим
const string alphabet = "ab0123"; // FsCheck подбирает алфавит сам, по типу string
for (var i = 0; i < 1000; i++)
{
    var s = new string(Enumerable.Range(0, rnd.Next(8))
        .Select(_ => alphabet[rnd.Next(alphabet.Length)]).ToArray());
    if (!Rle.TryDecode(Rle.Encode(s), out var back) || back != s)
    {
        // FsCheck ещё и «ужал» бы контрпример; фаззер Go делает это сам
        // и сохраняет вход в testdata/fuzz — следующий go test его повторит.
        Console.WriteLine($"контрпример: \"{s}\"");
        break;
    }
}

static class Rle
{
    public static string Encode(string s)
    {
        var sb = new StringBuilder();
        for (var i = 0; i < s.Length;)
        {
            var j = i;
            while (j < s.Length && s[j] == s[i]) j++;
            sb.Append(j - i).Append(s[i]);
            i = j;
        }
        return sb.ToString();
    }

    // Та же ошибка, что в примере на Go: цифры во входе ломают формат.
    public static bool TryDecode(string s, out string result)
    {
        var sb = new StringBuilder();
        result = "";
        for (var i = 0; i < s.Length;)
        {
            var j = i;
            while (j < s.Length && char.IsAsciiDigit(s[j])) j++;
            if (j == i || j == s.Length) return false;
            sb.Append(s[j], int.Parse(s[i..j]));
            i = j + 1;
        }
        result = sb.ToString();
        return true;
    }
}
