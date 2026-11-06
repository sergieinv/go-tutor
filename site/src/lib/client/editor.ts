// Редактор Go на CodeMirror 6 с цветами из CSS-переменных сайта.
import { indentWithTab } from "@codemirror/commands";
import { go } from "@codemirror/lang-go";
import { HighlightStyle, indentUnit, syntaxHighlighting } from "@codemirror/language";
import { EditorState, StateEffect, StateField, type Extension } from "@codemirror/state";
import { Decoration, type DecorationSet, EditorView, keymap } from "@codemirror/view";
import { tags as t } from "@lezer/highlight";
import { basicSetup } from "codemirror";

export interface EditorOptions {
  doc: string;
  /** Ctrl/Cmd+Enter — главное действие: «Запустить» или «Проверить». */
  onSubmit?: () => void;
  onChange?: (doc: string) => void;
  readOnly?: boolean;
  label: string;
}

const theme = EditorView.theme({
  "&": {
    fontSize: "14px",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius)",
    backgroundColor: "var(--bg)",
    color: "var(--fg)",
  },
  "&.cm-focused": { outline: "2px solid var(--accent)", outlineOffset: "-1px" },
  ".cm-scroller": { fontFamily: "var(--mono)", lineHeight: "1.5" },
  ".cm-content": { caretColor: "var(--fg)" },
  ".cm-gutters": {
    backgroundColor: "var(--bg-soft)",
    color: "var(--fg-muted)",
    border: "none",
    borderRadius: "var(--radius) 0 0 var(--radius)",
  },
  ".cm-viz-line": { backgroundColor: "color-mix(in srgb, var(--warn-border) 28%, transparent)" },
  ".cm-activeLine, .cm-activeLineGutter": { backgroundColor: "color-mix(in srgb, var(--accent) 7%, transparent)" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground": {
    backgroundColor: "color-mix(in srgb, var(--accent) 25%, transparent) !important",
  },
});

// Цвета токенов — CSS-переменные --syn-*, поэтому подсветка следует теме сайта.
const highlight = HighlightStyle.define([
  { tag: [t.keyword, t.controlKeyword, t.definitionKeyword, t.moduleKeyword, t.operatorKeyword], color: "var(--syn-keyword)" },
  { tag: [t.string, t.special(t.string), t.character], color: "var(--syn-string)" },
  { tag: [t.number, t.bool, t.null, t.atom], color: "var(--syn-constant)" },
  { tag: [t.comment, t.lineComment, t.blockComment], color: "var(--syn-comment)", fontStyle: "italic" },
  { tag: [t.typeName, t.standard(t.typeName)], color: "var(--syn-type)" },
  { tag: [t.function(t.variableName), t.function(t.propertyName), t.definition(t.function(t.variableName))], color: "var(--syn-function)" },
  { tag: [t.definition(t.variableName), t.definition(t.propertyName)], color: "var(--syn-definition)" },
  { tag: [t.operator, t.punctuation], color: "var(--fg)" },
]);

// Подсветка строк по шагу визуализации.
const setHighlight = StateEffect.define<number[]>();
const lineMark = Decoration.line({ class: "cm-viz-line" });
const highlightField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(deco, tr) {
    deco = deco.map(tr.changes);
    for (const e of tr.effects) {
      if (!e.is(setHighlight)) continue;
      const lines = e.value.filter((n) => n >= 1 && n <= tr.state.doc.lines).sort((a, b) => a - b);
      deco = Decoration.set(lines.map((n) => lineMark.range(tr.state.doc.line(n).from)));
    }
    return deco;
  },
  provide: (f) => EditorView.decorations.from(f),
});

/** Подсвечивает строки (с 1) и прокручивает редактор к первой; пустой список снимает подсветку. */
export function highlightLines(view: EditorView, lines: number[]) {
  const first = lines.find((n) => n >= 1 && n <= view.state.doc.lines);
  view.dispatch({
    effects: [
      setHighlight.of(lines),
      ...(first ? [EditorView.scrollIntoView(view.state.doc.line(first).from, { y: "nearest" })] : []),
    ],
  });
}

export function createEditor(parent: HTMLElement, opts: EditorOptions): EditorView {
  const extensions: Extension[] = [
    basicSetup,
    go(),
    syntaxHighlighting(highlight),
    indentUnit.of("\t"),
    EditorState.tabSize.of(4),
    theme,
    highlightField,
    // Tab — отступ, как в IDE. Уйти из редактора с клавиатуры: Esc, затем Tab
    // (встроенный режим CodeMirror, без него клавиатура «застревает» в редакторе).
    keymap.of([indentWithTab]),
    EditorView.contentAttributes.of({ "aria-label": `${opts.label}. Tab — отступ; чтобы выйти из редактора, нажмите Esc, затем Tab` }),
  ];
  if (opts.onSubmit) {
    const submit = opts.onSubmit;
    extensions.unshift(
      keymap.of([
        {
          key: "Mod-Enter",
          run: () => {
            submit();
            return true;
          },
        },
      ]),
    );
  }
  if (opts.onChange) {
    const change = opts.onChange;
    extensions.push(EditorView.updateListener.of((u) => u.docChanged && change(u.state.doc.toString())));
  }
  if (opts.readOnly) extensions.push(EditorState.readOnly.of(true), EditorView.editable.of(false));
  return new EditorView({ parent, state: EditorState.create({ doc: opts.doc, extensions }) });
}

export function setDoc(view: EditorView, doc: string) {
  view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: doc } });
}

export const getDoc = (view: EditorView) => view.state.doc.toString();
