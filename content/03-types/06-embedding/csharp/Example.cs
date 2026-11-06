var a = new Admin { Name = "ann", Level = 2 };

// Наследование: Admin является User, и virtual-вызов
// из базового класса попадает в метод наследника
Console.WriteLine(a.Describe()); // ann is admin — в Go было бы "ann is user"

User u = a; // в Go так нельзя: Admin не является User
Console.WriteLine(u.Role());     // admin

class User
{
    public string Name { get; set; } = "";
    public string Greet() => "hi, " + Name;
    public virtual string Role() => "user";
    public string Describe() => Name + " is " + Role();
}

class Admin : User
{
    public int Level { get; set; }
    public override string Role() => "admin";
}
