// .NET: автомат состояний соединения — switch-выражение по кортежу
// (состояние, пакет) с type patterns. В Go то же самое — map[state]map[id]handler
// или switch по состоянию с type switch внутри.
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;

var c = new Connection();
Console.WriteLine(c.Handle(new Handshake(767, 2)));        // Login
Console.WriteLine(c.Handle(new LoginStart("Steve")));       // Play
Console.WriteLine(Offline.Uuid("Steve"));                   // 5627dd98-e6be-3c21-b8a8-e92344183641
Console.WriteLine(Status.Json("1.21.1", 767, 20, 1));
try { c.Handle(new StatusRequest()); }
catch (InvalidOperationException e) { Console.WriteLine(e.Message); }

enum State { Handshake, Status, Login, Play, Closed }

record Handshake(int Protocol, int NextState);
record StatusRequest;
record LoginStart(string Name);

class Connection
{
    public State State { get; private set; } = State.Handshake;

    // Неподходящая пара (состояние, пакет) попадает в «_» — исключение
    // и закрытие. В Go вместо исключения — ошибка из обработчика.
    public State Handle(object packet) => State = (State, packet) switch
    {
        (State.Handshake, Handshake { NextState: 1 }) => State.Status,
        (State.Handshake, Handshake { NextState: 2, Protocol: 767 }) => State.Login,
        (State.Status, StatusRequest) => State.Status,
        (State.Login, LoginStart) => State.Play,
        _ => throw new InvalidOperationException($"unexpected {packet.GetType().Name} in {State}"),
    };
}

static class Offline
{
    // UUID v3 offline-игрока. Байты MD5 идут в порядке RFC 4122 (big-endian),
    // поэтому Guid строится с bigEndian: true (.NET 8+); обычный new Guid(bytes)
    // переставил бы первые три поля (little-endian, как в Windows GUID).
    public static Guid Uuid(string name)
    {
        byte[] h = MD5.HashData(Encoding.UTF8.GetBytes("OfflinePlayer:" + name));
        h[6] = (byte)((h[6] & 0x0f) | 0x30); // версия 3
        h[8] = (byte)((h[8] & 0x3f) | 0x80); // вариант RFC 4122
        return new Guid(h, bigEndian: true);
    }
}

static class Status
{
    // Ответ на пинг списка серверов; имена полей — camelCase, как ждёт клиент.
    public static string Json(string version, int protocol, int max, int online) =>
        JsonSerializer.Serialize(new
        {
            version = new { name = version, protocol },
            players = new { max, online },
            description = new { text = "Учебный сервер" },
        });
}
