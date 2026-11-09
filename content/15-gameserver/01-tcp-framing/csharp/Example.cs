// .NET: кадрирование на System.IO.Pipelines. PipeReader отдаёт всё, что
// пришло, а разбор говорит, сколько байт он «съел» (consumed) и сколько
// просмотрел (examined). В Go то же самое делают bufio.Reader + io.ReadFull:
// читатель блокируется, пока байтов не хватит, и состояние разбора
// живёт в обычных локальных переменных, а не в позициях буфера.
using System.Buffers;
using System.IO.Pipelines;

const int MaxFrame = 1 << 16;

// BinaryReader.Read7BitEncodedInt — тот же VarInt: 7 бит на байт,
// старший бит — продолжение, не больше 5 байт. Неканоническую
// запись 80 00 он принимает как 0; лимит 5 байт и лишние биты в
// пятом байте — FormatException.
using (var br = new BinaryReader(new MemoryStream([0xdd, 0xc7, 0x01])))
    Console.WriteLine(br.Read7BitEncodedInt()); // 25565
using (var br = new BinaryReader(new MemoryStream([0x80, 0x00])))
    Console.WriteLine(br.Read7BitEncodedInt()); // 0 — неканоническая форма прошла
try
{
    using var br = new BinaryReader(new MemoryStream([0xff, 0xff, 0xff, 0xff, 0x1f]));
    br.Read7BitEncodedInt();
}
catch (FormatException) { Console.WriteLine("FormatException"); }

// Три кадра приходят кусками по 1, 2, 3… байта.
var pipe = new Pipe();
byte[] wire = [0x03, 0x01, (byte)'h', (byte)'i', 0x01, 0x02, 0x02, 0x01, (byte)'!'];
var writer = Task.Run(async () =>
{
    for (int i = 0, size = 1; i < wire.Length; i += size, size++)
        await pipe.Writer.WriteAsync(wire.AsMemory(i, Math.Min(size, wire.Length - i)));
    await pipe.Writer.CompleteAsync();
});

while (true)
{
    ReadResult result = await pipe.Reader.ReadAsync();
    ReadOnlySequence<byte> buffer = result.Buffer;
    // Разбираем столько целых кадров, сколько уже есть в буфере.
    while (TryReadFrame(ref buffer, out var body))
        Console.WriteLine($"кадр: id={body.FirstSpan[0]}, {body.Length - 1} байт данных");
    // Хвост неполного кадра остаётся в трубе до следующего ReadAsync.
    pipe.Reader.AdvanceTo(buffer.Start, buffer.End);
    if (result.IsCompleted) break;
}
await writer;

static bool TryReadFrame(ref ReadOnlySequence<byte> buffer, out ReadOnlySequence<byte> body)
{
    body = default;
    var reader = new SequenceReader<byte>(buffer);
    int length = 0;
    for (int i = 0; ; i++)
    {
        if (i == 5) throw new InvalidDataException("VarInt длиннее 5 байт");
        if (!reader.TryRead(out byte b)) return false; // мало байт — ждём ещё
        length |= (b & 0x7f) << (7 * i);
        if ((b & 0x80) == 0) break;
    }
    // Проверка длины — до того, как ждать или выделять что-либо под тело.
    if (length < 1 || length > MaxFrame) throw new InvalidDataException("неверная длина кадра");
    if (reader.Remaining < length) return false;
    body = buffer.Slice(reader.Position, length);
    buffer = buffer.Slice(body.End);
    return true;
}
