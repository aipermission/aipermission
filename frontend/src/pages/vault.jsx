import { Notice } from "../components/ui/notice";
import { useVaultBindings } from "../components/vault/use-vault-bindings";
import { useVaultCollection } from "../components/vault/use-vault-collection";
import { useVaultValueActions } from "../components/vault/use-vault-value-actions";
import { VaultFilters, VaultItemsTable, VaultPageHeader } from "../components/vault/vault-page-content";
import { VaultValueDialogs } from "../components/vault/vault-value-dialogs";
import { VaultBindingsDialog, VaultEditor } from "./vault-components";

export function VaultPage() {
  const collection = useVaultCollection();
  const values = useVaultValueActions({ reloadItems: collection.loadItems, setAction: collection.setAction });
  const bindings = useVaultBindings({ setAction: collection.setAction });

  return (
    <section className="mx-auto grid w-full max-w-7xl gap-5">
      <VaultPageHeader
        loading={collection.items.state === "loading"}
        canCreate={collection.projects.data.length > 0}
        onRefresh={() => void collection.loadItems()}
        onCreate={collection.openCreate}
      />

      <VaultFilters filters={collection.filters} projects={collection.projects.data} onChange={collection.setFilters} />

      <VaultNotices collection={collection} />

      <VaultItemsTable
        items={collection.items}
        visibleItems={collection.visibleItems}
        projects={collection.projects.data}
        onEdit={collection.openEdit}
        onReveal={(item) => void values.openReveal(item)}
        onReplace={values.openReplace}
        onBindings={(item) => void bindings.openBindings(item)}
        onDelete={values.openRemove}
      />

      <VaultEditor
        editor={collection.editor}
        projects={collection.projects.data}
        action={collection.action}
        onChange={collection.setEditor}
        onClose={collection.closeEditor}
        onSubmit={collection.saveItem}
      />

      <VaultBindingsDialog
        state={bindings.bindings}
        projects={collection.projects.data}
        onChange={bindings.setBindings}
        onClose={bindings.closeBindings}
        onSave={bindings.saveBinding}
        onDelete={(item) => void bindings.deleteBinding(item)}
      />

      <VaultValueDialogs owner={values} />
    </section>
  );
}

function VaultNotices({ collection }) {
  const { action, editor, items, projects } = collection;
  return (
    <>
      {items.state === "error" ? <Notice tone="bad">{items.error}</Notice> : null}
      {projects.state === "error" ? <Notice tone="bad">{projects.error}</Notice> : null}
      {action.message ? <Notice tone="good">{action.message}</Notice> : null}
      {action.error && !editor.open ? <Notice tone="bad">{action.error}</Notice> : null}
      {items.state === "ready" && items.total > items.data.length ? (
        <Notice tone="warn">
          Showing {items.data.length} of {items.total} matching Vault items. Refine the project or search filter to reach items outside this
          bounded result.
        </Notice>
      ) : null}
    </>
  );
}
