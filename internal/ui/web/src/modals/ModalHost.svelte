<script lang="ts">
  import { modal } from '$stores/modals';
  import DomainModal from './DomainModal.svelte';
  import GroupModal from './GroupModal.svelte';
  import AddSiteModal from './AddSiteModal.svelte';
  import PresetModal from './PresetModal.svelte';
  import RemoteControlModal from './RemoteControlModal.svelte';
  import LANProgressModal from './LANProgressModal.svelte';
  import AddPhpModal from './AddPhpModal.svelte';
  import RebuildPhpModal from './RebuildPhpModal.svelte';
  import ConfirmEnvSaveModal from './ConfirmEnvSaveModal.svelte';
  import ConfirmEnvRestoreModal from './ConfirmEnvRestoreModal.svelte';
  import EnvProposeModal from './EnvProposeModal.svelte';
  import ConfirmNginxSaveModal from './ConfirmNginxSaveModal.svelte';
  import ConfirmNginxRestoreModal from './ConfirmNginxRestoreModal.svelte';
  import ConfirmNginxResetModal from './ConfirmNginxResetModal.svelte';
  import ConfirmNginxGlobalSaveModal from './ConfirmNginxGlobalSaveModal.svelte';
  import ConfirmNginxGlobalRestoreModal from './ConfirmNginxGlobalRestoreModal.svelte';
  import ConfirmNginxGlobalResetModal from './ConfirmNginxGlobalResetModal.svelte';
  import ConfirmPhpRemoveModal from './ConfirmPhpRemoveModal.svelte';
  import ConfirmWorkspaceDeleteModal from './ConfirmWorkspaceDeleteModal.svelte';
  import ConfirmTuningSaveModal from './ConfirmTuningSaveModal.svelte';
  import ConfirmTuningRestoreModal from './ConfirmTuningRestoreModal.svelte';
  import ConfirmTuningResetModal from './ConfirmTuningResetModal.svelte';
  import ConfirmSiteUnlinkModal from './ConfirmSiteUnlinkModal.svelte';
  import ConfirmServiceInstallModal from './ConfirmServiceInstallModal.svelte';
  import ConfirmCronDeleteModal from './ConfirmCronDeleteModal.svelte';
  import ConfirmFileDeleteModal from './ConfirmFileDeleteModal.svelte';
  import FixPermissionsModal from './FixPermissionsModal.svelte';
  import ConfirmSFTPWithdrawModal from './ConfirmSFTPWithdrawModal.svelte';
  import ErrorModal from './ErrorModal.svelte';
</script>

{#if $modal.kind === 'domain' && $modal.site}
  <DomainModal site={$modal.site} />
{:else if $modal.kind === 'group' && $modal.site}
  <GroupModal site={$modal.site} />
{:else if $modal.kind === 'addSite'}
  <AddSiteModal />
{:else if $modal.kind === 'preset'}
  <PresetModal />
{:else if $modal.kind === 'remoteControl'}
  <RemoteControlModal />
{:else if $modal.kind === 'lanProgress'}
  <LANProgressModal />
{:else if $modal.kind === 'phpAdd'}
  <AddPhpModal />
{:else if $modal.kind === 'phpRebuild' && $modal.phpRebuild}
  <RebuildPhpModal version={$modal.phpRebuild.version} />
{:else if $modal.kind === 'envSave' && $modal.envSave}
  <ConfirmEnvSaveModal />
{:else if $modal.kind === 'envRestore' && $modal.envRestore}
  <ConfirmEnvRestoreModal />
{:else if $modal.kind === 'envPropose' && $modal.envPropose}
  <EnvProposeModal />
{:else if $modal.kind === 'nginxSave' && $modal.nginxSave}
  <ConfirmNginxSaveModal />
{:else if $modal.kind === 'nginxRestore' && $modal.nginxRestore}
  <ConfirmNginxRestoreModal />
{:else if $modal.kind === 'nginxReset' && $modal.nginxReset}
  <ConfirmNginxResetModal />
{:else if $modal.kind === 'nginxGlobalSave' && $modal.nginxGlobalSave}
  <ConfirmNginxGlobalSaveModal />
{:else if $modal.kind === 'nginxGlobalRestore' && $modal.nginxGlobalRestore}
  <ConfirmNginxGlobalRestoreModal />
{:else if $modal.kind === 'nginxGlobalReset' && $modal.nginxGlobalReset}
  <ConfirmNginxGlobalResetModal />
{:else if $modal.kind === 'phpRemove' && $modal.phpRemove}
  <ConfirmPhpRemoveModal />
{:else if $modal.kind === 'tuningSave' && $modal.tuningSave}
  <ConfirmTuningSaveModal />
{:else if $modal.kind === 'tuningRestore' && $modal.tuningRestore}
  <ConfirmTuningRestoreModal />
{:else if $modal.kind === 'tuningReset' && $modal.tuningReset}
  <ConfirmTuningResetModal />
{:else if $modal.kind === 'workspaceDelete' && $modal.workspaceDelete}
  <ConfirmWorkspaceDeleteModal />
{:else if $modal.kind === 'siteUnlink' && $modal.siteUnlink}
  <ConfirmSiteUnlinkModal />
{:else if $modal.kind === 'serviceInstall' && $modal.serviceInstall}
  <ConfirmServiceInstallModal />
{:else if $modal.kind === 'cronDelete' && $modal.cronDelete}
  <ConfirmCronDeleteModal />
{:else if $modal.kind === 'fileDelete' && $modal.fileDelete}
  <ConfirmFileDeleteModal />
{:else if $modal.kind === 'filePermissions' && $modal.filePermissions}
  <FixPermissionsModal />
{:else if $modal.kind === 'sftpWithdraw' && $modal.sftpWithdraw}
  <ConfirmSFTPWithdrawModal />
{:else if $modal.kind === 'error' && $modal.error}
  <ErrorModal />
{/if}
