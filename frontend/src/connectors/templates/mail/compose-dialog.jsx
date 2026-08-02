import { Bold, Code2, Heading2, Italic, Link, List, ListOrdered, Quote, Send, Underline } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Field, Input, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { recipientList, validateComposeFields } from "./helpers";
import { normalizeEditorLink, plainTextToHTML, richTextToPlainText, splitPlainTextLines } from "./rich-text";

export function ComposeDialog({ draft, busy, error, onClose, onSubmit }) {
  const [mode, setMode] = useState("plain");
  const [form, setForm] = useState(emptyComposeForm());
  const [formattedFallback, setFormattedFallback] = useState("");
  const [validationError, setValidationError] = useState("");

  useEffect(() => {
    if (!draft?.open) return;
    const nextForm = composeFormValue(draft.form);
    setMode(nextForm.html_body ? "formatted" : "plain");
    setForm(nextForm);
    setFormattedFallback(nextForm.html_body ? nextForm.text_body : "");
    setValidationError("");
  }, [draft?.open, draft?.reply, draft?.messageRef, draft?.pendingRequestID]);

  function updateForm(changes) {
    setValidationError("");
    setForm((current) => ({ ...current, ...changes }));
  }

  function selectFormattedMode() {
    if (mode === "formatted") return;
    setValidationError("");
    setForm((current) => {
      if (current.html_body && current.text_body === formattedFallback) return current;
      return { ...current, html_body: plainTextToHTML(current.text_body) };
    });
    setMode("formatted");
  }

  function submit(event) {
    event.preventDefault();
    const fields = {
      to: recipientList(form.to),
      cc: recipientList(form.cc),
      bcc: recipientList(form.bcc),
      subject: form.subject,
      text_body: form.text_body,
      html_body: mode === "formatted" ? form.html_body : "",
    };
    const validation = validateComposeFields(fields, { reply: Boolean(draft?.reply) });
    if (validation) {
      setValidationError(validation);
      return;
    }
    setValidationError("");
    onSubmit(fields);
  }

  return (
    <Dialog open={Boolean(draft?.open)} title={draft?.reply ? "Reply" : "Compose message"} description="Review every recipient and the complete bounded body before submitting." onClose={onClose} closeOnOverlay={false} size="xl" bodyClassName="min-h-0 overflow-auto">
      <form className="grid gap-4" onSubmit={submit}>
        <Notice tone="warn">SMTP acceptance does not guarantee delivery. An unknown submission result must not be retried automatically.</Notice>
        <AddressField label="To" value={form.to} onChange={(value) => updateForm({ to: value })} required />
        <div className="grid gap-3 sm:grid-cols-2">
          <AddressField label="CC" value={form.cc} onChange={(value) => updateForm({ cc: value })} />
          <AddressField label="BCC" value={form.bcc} onChange={(value) => updateForm({ bcc: value })} />
        </div>
        <Field>
          Subject
          <Input value={form.subject} onChange={(event) => updateForm({ subject: event.target.value })} required />
        </Field>
        <div className="inline-flex w-fit rounded-md border border-stone-300 p-1">
          <Button type="button" variant={mode === "plain" ? "primary" : "ghost"} className="h-8" onClick={() => { setMode("plain"); setValidationError(""); }}>Plain text</Button>
          <Button type="button" variant={mode === "formatted" ? "primary" : "ghost"} className="h-8" onClick={selectFormattedMode}>Formatted</Button>
        </div>
        {mode === "plain" ? (
          <Field>
            Message
            <Textarea className="min-h-64 resize-y" value={form.text_body} onChange={(event) => updateForm({ text_body: event.target.value })} required />
          </Field>
        ) : (
          <>
            <RichTextEditor value={form.html_body} onChange={(html, text) => {
              setFormattedFallback(text);
              updateForm({ html_body: html, text_body: text });
            }} />
            <Field>
              Plain-text fallback
              <Textarea className="min-h-28 resize-y font-mono text-xs" value={form.text_body} readOnly />
              <span className="text-xs font-normal text-stone-500">This complete fallback is included in the approval preview and multipart message.</span>
            </Field>
          </>
        )}
        {validationError || error ? <Notice tone="bad">{validationError || error}</Notice> : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose}>Cancel</Button>
          <Button type="submit" disabled={busy || recipientList(form.to).length === 0 || !form.subject.trim() || !form.text_body.trim()}>
            <Send className="h-4 w-4" />
            {busy ? "Submitting..." : draft?.reply ? "Send reply" : "Send message"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function RichTextEditor({ value, onChange }) {
  const editorRef = useRef(null);
  const savedRange = useRef(null);
  const [linkEditor, setLinkEditor] = useState({ open: false, value: "https://", error: "" });
  useEffect(() => {
    if (editorRef.current && editorRef.current.innerHTML !== value) editorRef.current.innerHTML = value;
  }, [value]);

  function command(name, argument = null) {
    editorRef.current?.focus();
    document.execCommand(name, false, argument);
    emitValue();
  }

  function openLinkEditor() {
    editorRef.current?.focus();
    const selection = window.getSelection();
    savedRange.current = selection?.rangeCount ? selection.getRangeAt(0).cloneRange() : null;
    setLinkEditor({ open: true, value: "https://", error: "" });
  }

  function applyLink(event) {
    event.preventDefault();
    if (!savedRange.current || savedRange.current.collapsed) {
      setLinkEditor((current) => ({ ...current, error: "Select message text before adding a link." }));
      return;
    }
    const href = normalizeEditorLink(linkEditor.value);
    if (!href) {
      setLinkEditor((current) => ({ ...current, error: "Enter an http, https, or mailto URL." }));
      return;
    }
    const selection = window.getSelection();
    if (selection && savedRange.current) {
      selection.removeAllRanges();
      selection.addRange(savedRange.current);
    }
    command("createLink", href);
    savedRange.current = null;
    setLinkEditor({ open: false, value: "https://", error: "" });
  }

  function emitValue() {
    const editor = editorRef.current;
    if (!editor) return;
    onChange(editor.innerHTML, richTextToPlainText(editor));
  }

  function pastePlainText(event) {
    event.preventDefault();
    insertPlainText(event.clipboardData?.getData("text/plain") || "");
    emitValue();
  }

  return (
    <Field>
      Message
      <div className="overflow-hidden rounded-md border border-stone-300 bg-white">
        <div className="flex flex-wrap gap-1 border-b border-stone-200 p-1">
          <FormatButton title="Bold" onClick={() => command("bold")}><Bold className="h-4 w-4" /></FormatButton>
          <FormatButton title="Italic" onClick={() => command("italic")}><Italic className="h-4 w-4" /></FormatButton>
          <FormatButton title="Underline" onClick={() => command("underline")}><Underline className="h-4 w-4" /></FormatButton>
          <FormatButton title="Heading" onClick={() => command("formatBlock", "h2")}><Heading2 className="h-4 w-4" /></FormatButton>
          <FormatButton title="Bulleted list" onClick={() => command("insertUnorderedList")}><List className="h-4 w-4" /></FormatButton>
          <FormatButton title="Numbered list" onClick={() => command("insertOrderedList")}><ListOrdered className="h-4 w-4" /></FormatButton>
          <FormatButton title="Quote" onClick={() => command("formatBlock", "blockquote")}><Quote className="h-4 w-4" /></FormatButton>
          <FormatButton title="Code block" onClick={() => command("formatBlock", "pre")}><Code2 className="h-4 w-4" /></FormatButton>
          <FormatButton title="Link" onClick={openLinkEditor}><Link className="h-4 w-4" /></FormatButton>
        </div>
        {linkEditor.open ? (
          <div className="flex flex-wrap items-center gap-2 border-b border-stone-200 p-2">
            <Input className="h-8 min-w-52 flex-1" value={linkEditor.value} onChange={(event) => setLinkEditor({ open: true, value: event.target.value, error: "" })} onKeyDown={(event) => { if (event.key === "Enter") applyLink(event); }} aria-label="Link URL" autoFocus />
            <Button type="button" className="h-8" onClick={applyLink}>Apply link</Button>
            <Button type="button" variant="outline" className="h-8" onClick={() => setLinkEditor({ open: false, value: "https://", error: "" })}>Cancel</Button>
            {linkEditor.error ? <span className="w-full text-xs text-red-600">{linkEditor.error}</span> : null}
          </div>
        ) : null}
        <div ref={editorRef} contentEditable suppressContentEditableWarning className="min-h-64 whitespace-pre-wrap p-3 text-sm outline-none" onInput={emitValue} onPaste={pastePlainText} onDrop={(event) => event.preventDefault()} />
      </div>
      <span className="text-xs font-normal text-stone-500">Only basic formatting is accepted. The gateway sanitizes formatted content before approval and SMTP submission.</span>
    </Field>
  );
}

function insertPlainText(value) {
  const selection = window.getSelection();
  if (!selection || selection.rangeCount === 0) return;
  const range = selection.getRangeAt(0);
  range.deleteContents();
  const fragment = document.createDocumentFragment();
  splitPlainTextLines(value).forEach((line, index) => {
    if (index > 0) fragment.appendChild(document.createElement("br"));
    if (line) fragment.appendChild(document.createTextNode(line));
  });
  const lastNode = fragment.lastChild;
  if (!lastNode) return;
  range.insertNode(fragment);
  range.setStartAfter(lastNode);
  range.collapse(true);
  selection.removeAllRanges();
  selection.addRange(range);
}

function FormatButton({ title, onClick, children }) {
  return <Button type="button" variant="ghost" className="h-8 w-8 px-0" title={title} aria-label={title} onMouseDown={(event) => event.preventDefault()} onClick={onClick}>{children}</Button>;
}

function AddressField({ label, value, onChange, required = false }) {
  return <Field>{label}<Input value={value} onChange={(event) => onChange(event.target.value)} placeholder="name@example.com" required={required} /></Field>;
}

function emptyComposeForm() {
  return { to: "", cc: "", bcc: "", subject: "", text_body: "", html_body: "" };
}

function composeFormValue(value = {}) {
  const form = { ...emptyComposeForm(), ...value };
  for (const field of ["to", "cc", "bcc"]) {
    if (Array.isArray(form[field])) form[field] = form[field].join(", ");
  }
  return form;
}
