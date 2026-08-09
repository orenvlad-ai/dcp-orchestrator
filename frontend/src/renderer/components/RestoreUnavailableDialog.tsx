import * as Dialog from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { isOrchestratorSession } from "../types/workspace";
import type { WorkspaceSession } from "../types/workspace";
import { Button } from "./ui/button";
import {
	settingsDialogContentClass,
	settingsDialogFooterClass,
	settingsDialogHeaderClass,
} from "./ui/dialog";

type RestoreUnavailableDialogProps = {
	open: boolean;
	session: WorkspaceSession;
	onOpenChange: (open: boolean) => void;
	onRecreated: (newOrchestratorId: string) => void;
};

export function RestoreUnavailableDialog({ open, session, onOpenChange }: RestoreUnavailableDialogProps) {
	const { t } = useTranslation();
	const orchestrator = isOrchestratorSession(session);

	return (
		<Dialog.Root open={open} onOpenChange={onOpenChange}>
			<Dialog.Portal>
				<Dialog.Overlay className="dialog-overlay data-[state=open]:animate-overlay-in" />
				<Dialog.Content
					className={`${settingsDialogContentClass} fixed left-1/2 top-1/2 w-dialog-md -translate-x-1/2 -translate-y-1/2 data-[state=open]:animate-modal-in`}
				>
					<button
						type="button"
						className="settings-dialog-close-button settings-close-button"
						aria-label={t("common.close")}
						onClick={() => onOpenChange(false)}
					>
						<X className="size-5" aria-hidden="true" />
					</button>
					<div className={settingsDialogHeaderClass}>
						<Dialog.Title className="settings-dialog-title">{t("restoreUnavailable.title")}</Dialog.Title>
						<Dialog.Description className="text-control text-settings-muted">
							{t("restoreUnavailable.sessionBody")}
						</Dialog.Description>
					</div>
					<div className={settingsDialogFooterClass}>
						<Button type="button" variant="footer" onClick={() => onOpenChange(false)}>
							{orchestrator ? t("confirm.cancel") : t("restoreUnavailable.close")}
						</Button>
					</div>
				</Dialog.Content>
			</Dialog.Portal>
		</Dialog.Root>
	);
}
