import { useState } from 'react'
import { Bot, KeyRound, Plus, Trash2, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  createRobot,
  deleteRobot,
  registryHost,
  rotateRobotSecret,
  type RobotInfo,
  type RobotRole,
  type RobotWithSecret,
} from '@/api'
import { keys, useInvalidate, useRobots } from '@/lib/queries'
import {
  Command,
  ConfirmDialog,
  CopyButton,
  EmptyState,
  Field,
  FormDialog,
  NativeSelect,
  Panel,
  QueryPanel,
  RoleBadge,
  RowAction,
  Section,
  splitList,
  StatusBadge,
  Timestamp,
} from '@/components/kit'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

const ROBOT_ROLES: RobotRole[] = ['pusher', 'reader']
const LIFETIMES = [30, 90, 365, 0] // days; 0 = never
const DEFAULT_LIFETIME = 90
const NAME_PATTERN = /^[a-z0-9]+(?:[._-][a-z0-9]+)*$/
const SOON_MS = 7 * 24 * 3600 * 1000

function LifetimeField({ value, onChange }: { value: number; onChange: (days: number) => void }) {
  const { t } = useTranslation()
  return (
    <Field label={t('robots.expires')} hint={t('robots.expiresHint')}>
      {ids => (
        <NativeSelect {...ids} value={value} onChange={e => onChange(Number(e.target.value))}>
          {LIFETIMES.map(days => (
            <option key={days} value={days}>
              {days === 0 ? t('robots.never') : t('robots.days', { count: days })}
            </option>
          ))}
        </NativeSelect>
      )}
    </Field>
  )
}

function CreateRobotDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (robot: RobotWithSecret) => void }) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [role, setRole] = useState<RobotRole>('pusher')
  const [patterns, setPatterns] = useState('')
  const [lifetime, setLifetime] = useState(DEFAULT_LIFETIME)

  const nameOk = NAME_PATTERN.test(name) && name.length <= 64

  return (
    <FormDialog
      title={t('robots.new')}
      description={t('robots.newIntro')}
      submitLabel={t('robots.create')}
      onSubmit={async () => {
        onCreated(
          await createRobot({
            name,
            description: description.trim(),
            role,
            repo_patterns: splitList(patterns),
            expires_in_days: lifetime,
          }),
        )
      }}
      onClose={onClose}
      canSubmit={nameOk}
    >
      <Field label={t('robots.name')} hint={t('robots.nameHint', { username: `robot+${name || 'name'}` })}>
        {ids => (
          <Input
            {...ids}
            value={name}
            onChange={e => setName(e.target.value.toLowerCase())}
            placeholder="ci-team-a"
            className="font-mono"
            autoComplete="off"
            spellCheck={false}
            aria-invalid={name !== '' && !nameOk}
            required
          />
        )}
      </Field>
      <Field label={t('robots.descriptionField')} optional>
        {ids => (
          <Input {...ids} value={description} onChange={e => setDescription(e.target.value)} placeholder={t('robots.descriptionPlaceholder')} maxLength={200} />
        )}
      </Field>
      <Field label={t('access.role')} hint={t(`robots.roleHint_${role}`)}>
        {ids => (
          <NativeSelect {...ids} value={role} onChange={e => setRole(e.target.value as RobotRole)}>
            {ROBOT_ROLES.map(item => (
              <option key={item} value={item}>
                {t(`roles.${item}`)}
              </option>
            ))}
          </NativeSelect>
        )}
      </Field>
      <Field label={t('access.repositories')} hint={t('robots.repositoriesHint')} optional>
        {ids => <Input {...ids} value={patterns} onChange={e => setPatterns(e.target.value)} placeholder="team-a/*" className="font-mono" />}
      </Field>
      <LifetimeField value={lifetime} onChange={setLifetime} />
    </FormDialog>
  )
}

function RotateDialog({ robot, onClose, onRotated }: { robot: RobotInfo; onClose: () => void; onRotated: (robot: RobotWithSecret) => void }) {
  const { t } = useTranslation()
  const [lifetime, setLifetime] = useState(DEFAULT_LIFETIME)
  return (
    <FormDialog
      title={t('robots.rotateTitle', { name: robot.name })}
      description={t('robots.rotateBody')}
      submitLabel={t('robots.rotate')}
      onSubmit={async () => onRotated(await rotateRobotSecret(robot.id, lifetime))}
      onClose={onClose}
    >
      <LifetimeField value={lifetime} onChange={setLifetime} />
    </FormDialog>
  )
}

/** Shown once, right after creation or rotation: the only time the secret
 *  exists outside the CI system it is pasted into. */
function SecretDialog({ robot, onClose }: { robot: RobotWithSecret; onClose: () => void }) {
  const { t } = useTranslation()
  const host = registryHost()
  return (
    <Dialog open onOpenChange={open => !open && onClose()}>
      <DialogContent className="min-w-0 sm:max-w-xl" onInteractOutside={event => event.preventDefault()}>
        <DialogHeader>
          <DialogTitle>{t('robots.secretTitle', { name: robot.name })}</DialogTitle>
          <DialogDescription className="flex items-start gap-2">
            <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warning" />
            {t('robots.secretOnce')}
          </DialogDescription>
        </DialogHeader>
        {/* min-w-0: a grid child otherwise grows to fit the long commands. */}
        <div className="min-w-0 space-y-4 text-sm">
          <dl className="divide-y rounded-lg border">
            <div className="flex items-center justify-between gap-3 px-3 py-2">
              <dt className="text-muted-foreground">{t('fields.username')}</dt>
              <dd className="flex min-w-0 items-center gap-1">
                <code className="truncate text-xs">{robot.username}</code>
                <CopyButton value={robot.username} />
              </dd>
            </div>
            <div className="flex items-center justify-between gap-3 px-3 py-2">
              <dt className="text-muted-foreground">{t('robots.secret')}</dt>
              <dd className="flex min-w-0 items-center gap-1">
                <code className="truncate text-xs">{robot.secret}</code>
                <CopyButton value={robot.secret} label={t('robots.copySecret')} />
              </dd>
            </div>
          </dl>
          <div className="space-y-1.5">
            <p className="text-xs font-medium">{t('robots.useInCi')}</p>
            <Command>{`echo "$DOCKYARD_TOKEN" | docker login ${host} -u '${robot.username}' --password-stdin`}</Command>
          </div>
          <div className="space-y-1.5">
            <p className="text-xs font-medium">{t('robots.useInKubernetes')}</p>
            <Command>{`kubectl create secret docker-registry dockyard-pull --docker-server=${host} --docker-username='${robot.username}' --docker-password="$DOCKYARD_TOKEN"`}</Command>
          </div>
        </div>
        <DialogFooter>
          <Button onClick={onClose}>{t('robots.done')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Expiry({ value }: { value: string | null }) {
  const { t } = useTranslation()
  // Read once per mount: "expired" does not need to flip while you look at it.
  const [now] = useState(() => Date.now())
  if (!value) return <span className="text-muted-foreground">{t('robots.never')}</span>
  const left = new Date(value).getTime() - now
  if (left <= 0) return <StatusBadge tone="danger">{t('robots.expired')}</StatusBadge>
  return (
    <span className="inline-flex items-center gap-1.5">
      {left < SOON_MS && <StatusBadge tone="warning">{t('robots.expiresSoon')}</StatusBadge>}
      <Timestamp value={value} className="text-muted-foreground" />
    </span>
  )
}

export default function RobotsSection() {
  const { t } = useTranslation()
  const robots = useRobots()
  const invalidate = useInvalidate()
  const [creating, setCreating] = useState(false)
  const [rotating, setRotating] = useState<RobotInfo | null>(null)
  const [deleting, setDeleting] = useState<RobotInfo | null>(null)
  const [revealed, setRevealed] = useState<RobotWithSecret | null>(null)

  async function reveal(robot: RobotWithSecret) {
    setCreating(false)
    setRotating(null)
    setRevealed(robot)
    await invalidate(keys.robots)
  }

  return (
    <Section
      id="robots"
      title={t('robots.title')}
      description={t('robots.description')}
      actions={
        <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
          <Plus />
          {t('robots.new')}
        </Button>
      }
    >
      <QueryPanel
        query={robots}
        skeletonRows={2}
        isEmpty={data => data.length === 0}
        empty={<EmptyState icon={Bot} title={t('robots.emptyTitle')} description={t('robots.emptyBody')} />}
      >
        {data => (
          <Panel>
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="px-4">{t('robots.name')}</TableHead>
                  <TableHead>{t('access.role')}</TableHead>
                  <TableHead className="hidden md:table-cell">{t('access.repositories')}</TableHead>
                  <TableHead className="hidden text-right sm:table-cell">{t('robots.lastUsed')}</TableHead>
                  <TableHead className="text-right">{t('robots.expires')}</TableHead>
                  <TableHead className="w-20 px-4" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.map(robot => (
                  <TableRow key={robot.id}>
                    <TableCell className="max-w-64 px-4">
                      <span className="flex items-center gap-2">
                        <Bot className="size-4 shrink-0 text-muted-foreground" />
                        <span className="min-w-0">
                          <code className="block truncate text-xs font-medium">{robot.username}</code>
                          {robot.description && (
                            <span className="block truncate text-xs text-muted-foreground" title={robot.description}>
                              {robot.description}
                            </span>
                          )}
                        </span>
                      </span>
                    </TableCell>
                    <TableCell>
                      <RoleBadge role={robot.role} />
                    </TableCell>
                    <TableCell className="hidden max-w-72 md:table-cell">
                      {robot.repo_patterns.length === 0 ? (
                        <span className="text-muted-foreground">{t('access.allRepositories')}</span>
                      ) : (
                        <span className="flex flex-wrap gap-1">
                          {robot.repo_patterns.map(pattern => (
                            <StatusBadge key={pattern} mono>
                              {pattern}
                            </StatusBadge>
                          ))}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="hidden text-right text-xs sm:table-cell">
                      {robot.last_used_at ? (
                        <Timestamp value={robot.last_used_at} className="text-muted-foreground" />
                      ) : (
                        <span className="text-muted-foreground">{t('robots.neverUsed')}</span>
                      )}
                    </TableCell>
                    <TableCell className="text-right text-xs">
                      <Expiry value={robot.expires_at} />
                    </TableCell>
                    <TableCell className="px-4">
                      <span className="flex justify-end">
                        <RowAction label={t('robots.rotateNamed', { name: robot.name })} onClick={() => setRotating(robot)}>
                          <KeyRound />
                        </RowAction>
                        <RowAction label={t('robots.deleteNamed', { name: robot.name })} onClick={() => setDeleting(robot)} destructive>
                          <Trash2 />
                        </RowAction>
                      </span>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Panel>
        )}
      </QueryPanel>

      {creating && <CreateRobotDialog onClose={() => setCreating(false)} onCreated={reveal} />}
      {rotating && <RotateDialog robot={rotating} onClose={() => setRotating(null)} onRotated={reveal} />}
      {revealed && <SecretDialog robot={revealed} onClose={() => setRevealed(null)} />}
      {deleting && (
        <ConfirmDialog
          title={t('robots.deleteTitle', { name: deleting.name })}
          description={t('robots.deleteBody')}
          confirmLabel={t('robots.delete')}
          destructive
          onConfirm={async () => {
            await deleteRobot(deleting.id)
            toast.success(t('robots.deleted', { name: deleting.name }))
            setDeleting(null)
            await invalidate(keys.robots)
          }}
          onCancel={() => setDeleting(null)}
        />
      )}
    </Section>
  )
}
