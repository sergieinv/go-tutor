// Четыре разных цикла там, где в Go один for
for (int i = 0; i < 3; i++) Console.Write($"{i} ");

int n = 1;
while (n < 100) n *= 3;

int attempts = 0;
do { attempts++; } while (attempts < 4);

foreach (var i in Enumerable.Range(0, 6))
    if (i % 2 == 1) Console.Write($"{i} ");

// Выход из вложенного цикла — через goto или флаг
int[][] grid = [[1, 2, 3], [4, -5, 6], [7, 8, 9]];
for (int r = 0; r < grid.Length; r++)
    for (int c = 0; c < grid[r].Length; c++)
        if (grid[r][c] < 0) goto done;
done:
Console.WriteLine("found");

// Переменная for одна на весь цикл: все лямбды видят 3
var fns = new List<Func<int>>();
for (int i = 0; i < 3; i++) fns.Add(() => i * 10);
Console.WriteLine(string.Join(" ", fns.Select(f => f()))); // 30 30 30
