using System;
using System.Collections.Generic;
using System.IO;
using System.Xml.Serialization;

var b = new Book
{
    Id = "go-1",
    Title = new Title { Text = "Go & C#" },
    Authors = new() { "Анна", "Иван" },
};

var ser = new XmlSerializer(typeof(Book));
var sw = new StringWriter();
ser.Serialize(sw, b); // в отличие от Go, добавит <?xml ...?> и пространства имён xsi/xsd
Console.WriteLine(sw);

[XmlRoot("book")] // ≈ XMLName xml.Name `xml:"book"`
public class Book
{
    [XmlAttribute("id")] public string Id { get; set; } = "";       // `xml:"id,attr"`
    [XmlElement("title")] public Title Title { get; set; } = new();  // `xml:"title"`

    [XmlArray("authors"), XmlArrayItem("author")]                     // `xml:"authors>author"`
    public List<string> Authors { get; set; } = new();
}

public class Title
{
    [XmlAttribute("sub")] public bool Sub { get; set; }
    [XmlText] public string Text { get; set; } = "";                  // `xml:",chardata"`
}
