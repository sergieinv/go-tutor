using System.Diagnostics;

// ArgumentList — аргументы по одному, как exec.Command(name, args...).
// UseShellExecute = false: без оболочки; в Go иначе и не бывает.
var psi = new ProcessStartInfo("echo")
{
    RedirectStandardOutput = true,
    RedirectStandardError = true,
    RedirectStandardInput = true,
    UseShellExecute = false,
};
psi.ArgumentList.Add("$HOME");
psi.ArgumentList.Add("a b");

using (var p = Process.Start(psi)!)
{
    p.StandardInput.Close();
    // Чтение stdout и stderr — асинхронно, иначе возможна взаимоблокировка
    // на полном пайпе. В Go cmd.Stdout = &buf делает это за вас.
    var stdoutTask = p.StandardOutput.ReadToEndAsync();
    var stderrTask = p.StandardError.ReadToEndAsync();
    await p.WaitForExitAsync();
    Console.WriteLine($"stdout={await stdoutTask} exit={p.ExitCode}"); // код — свойство, а не ошибка
}

// Таймаут: WaitForExitAsync с токеном только перестаёт ждать — процесс жив.
// exec.CommandContext в Go убивает процесс сам.
using var cts = new CancellationTokenSource(TimeSpan.FromMilliseconds(200));
using var hang = Process.Start("/bin/sh", ["-c", "while :; do :; done"]);
try
{
    await hang.WaitForExitAsync(cts.Token);
}
catch (OperationCanceledException)
{
    hang.Kill(entireProcessTree: true);
    Console.WriteLine("таймаут, процесс убит вручную");
}
