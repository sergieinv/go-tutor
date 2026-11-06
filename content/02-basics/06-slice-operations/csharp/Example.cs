var langs = new List<string> { "go", "rust", "c#" };
Console.WriteLine(langs.Contains("rust"));   // методы самого списка
Console.WriteLine(langs.IndexOf("c#"));

var sorted = new List<string>(langs);        // копия — как slices.Clone
sorted.Sort();                               // сортировка на месте

var nums = new List<int> { 10, 20, 30, 40, 50 };
nums.Insert(1, 15);                          // меняет список, ничего не возвращает
nums.RemoveRange(2, 2);                      // аналог slices.Delete(nums, 2, 4)

// Array.Copy бросает исключение, если в приёмнике мало места;
// copy в Go молча копирует min(len(dst), len(src)).
int[] src = [1, 2, 3, 4, 5];
int[] dst = new int[3];
Array.Copy(src, dst, dst.Length);

// SequenceEqual — сравнение содержимого
Console.WriteLine(src.Take(3).SequenceEqual(dst)); // True
