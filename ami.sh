AMIUSER=$1
yum install -y xfsprogs
mv /etc/yum.repos.d/* ~/

# Create the filesystems 
parted /dev/xvdf --script 'mklabel msdos mkpart primary 1M 512M mkpart primary 512M -1s print quit'
mkfs.xfs -L BOOTFS -f /dev/xvdf1
pvcreate /dev/xvdf2
vgcreate -s 4 ami /dev/xvdf2
# Create the volumes 
lvcreate -n tmp -L 1G ami
lvcreate -n home -L 1G ami
lvcreate -n var -L 7G ami
lvcreate -n var_log -L 1G ami
lvcreate -n var_log_audit -L 512M ami
lvcreate -n swap -L 2G ami
lvcreate -n root -l 100%FREE ami
# Create the file systems 
mkfs.xfs /dev/ami/home
mkfs.xfs /dev/ami/var_log
mkfs.xfs /dev/ami/var_log_audit
mkfs.xfs /dev/ami/tmp
mkfs.xfs /dev/ami/var
mkfs.xfs /dev/ami/root
mkswap /dev/ami/swap

# mount file systems
mkdir -p /mnt/ec2-image
mount /dev/mapper/ami-root /mnt/ec2-image
mkdir -p /mnt/ec2-image/{tmp,home,var,boot}
mount /dev/mapper/ami-tmp /mnt/ec2-image/tmp
mount /dev/mapper/ami-home /mnt/ec2-image/home
mount /dev/mapper/ami-var /mnt/ec2-image/var
mkdir -p /mnt/ec2-image/var/log
mount /dev/mapper/ami-var_log /mnt/ec2-image/var/log
mkdir -p /mnt/ec2-image/var/log/audit
mount /dev/mapper/ami-var_log_audit /mnt/ec2-image/var/log/audit
mount /dev/xvdf1 /mnt/ec2-image/boot 
 
# make devices
mkdir -p /mnt/ec2-image/{dev,etc,proc,sys}
mkdir -p /mnt/ec2-image/var/{cache,log,lib/rpm}
mknod -m 622 /mnt/ec2-image/dev/console c 5 1
mknod -m 666 /mnt/ec2-image/dev/null c 1 3
mknod -m 666 /mnt/ec2-image/dev/zero c 1 5
mknod -m 444 /mnt/ec2-image/dev/urandom c 1 9
mount -o bind /dev /mnt/ec2-image/dev
mount -o bind /dev/pts /mnt/ec2-image/dev/pts
mount -o bind /dev/shm /mnt/ec2-image/dev/shm
mount -o bind /proc /mnt/ec2-image/proc
mount -o bind /sys /mnt/ec2-image/sys
 
# create fstab 
cat <<EOF > /mnt/ec2-image/etc/fstab
/dev/mapper/ami-root /                       xfs     defaults        0 0
LABEL=BOOTFS /boot                   xfs     defaults        0 0
/dev/mapper/ami-home /home                   xfs     defaults        0 0
/dev/mapper/ami-tmp  /tmp                    xfs     defaults        0 0
/dev/mapper/ami-var  /var                    xfs     defaults        0 0
/dev/mapper/ami-var_log /var/log                xfs     defaults        0 0
/dev/mapper/ami-var_log_audit /var/log/audit          xfs     defaults        0 0
/dev/mapper/ami-swap swap                    swap    defaults        0 0
EOF
 
# create a yum configuration for the installation 
mkdir -p /opt/ec2/yum
cat <<EOF> /opt/ec2/yum/yum.conf
[base]
name=Base
baseurl=http://mirror.centos.org/centos/7/os/x86_64/
gpgcheck=0
 
[updates]
name=Updates
baseurl=http://mirror.centos.org/centos/7/updates/x86_64/
gpgcheck=0
 
[extras]
name=Extras
baseurl=http://mirror.centos.org/centos/7/extras/x86_64/
gpgcheck=0
 
[puppetlabs-pc1]
name=Puppet Labs PC1 Repository el 7 
baseurl=http://yum.puppetlabs.com/el/7/PC1/x86_64/
gpgcheck=0
 
EOF

# Install the OS 
yum -c /opt/ec2/yum/yum.conf --installroot=/mnt/ec2-image -y install @core kernel openssh-clients grub2 grub2-tools lvm2 cloud-init puppet-agent ipa-client scap-security-guide
 
# Configure networking 
cat <<EOF > /mnt/ec2-image/etc/sysconfig/network
NETWORKING=yes
HOSTNAME=localhost.localdomain
EOF
cat <<EOF > /mnt/ec2-image/etc/sysconfig/network-scripts/ifcfg-eth0
DEVICE="eth0"
NM_CONTROLLED="no"
ONBOOT=yes
TYPE=Ethernet
BOOTPROTO=dhcp
DEFROUTE=yes
PEERDNS=no
PEERROUTES=yes
IPV4_FAILURE_FATAL=yes
IPV6INIT=no
EOF
 
# Setup cloud-init 
cat << EOF > /mnt/ec2-image/etc/cloud/cloud.cfg
users:
 - default
 
disable_root: 1
ssh_pwauth:   0
 
locale_configfile: /etc/sysconfig/i18n
mount_default_fields: [~, ~, 'auto', 'defaults,nofail', '0', '2']
resize_rootfs_tmp: /dev
ssh_deletekeys:   0
ssh_genkeytypes:  ~
syslog_fix_perms: ~
 
cloud_init_modules:
 - migrator
 - bootcmd
 - write-files
 - set_hostname
 - resolv_conf
 - rsyslog
 - users-groups
 - ssh
 
cloud_config_modules:
 - locale
 - set-passwords
 - yum-add-repo
 - package-update-upgrade-install
 - timezone
 - puppet
 - disable-ec2-metadata
 - runcmd
 
cloud_final_modules:
 - rightscale_userdata
 - scripts-per-once
 - scripts-per-boot
 - scripts-per-instance
 - scripts-user
 - ssh-authkey-fingerprints
 - keys-to-console
 - phone-home
 - final-message
 
system_info:
  default_user:
    name: $AMIUSER
    lock_passwd: true
    gecos: Inital Admin User
    groups: [wheel, adm, systemd-journal]
    sudo: ["ALL=(ALL) NOPASSWD:ALL"]
    shell: /bin/bash
  distro: rhel
  paths:
    cloud_dir: /var/lib/cloud
    templates_dir: /etc/cloud/templates
  ssh_svcname: sshd
 
datasource_list: [ Ec2, None]
 
# vim:syntax=yaml 
EOF
 
# Get console output at boot 
cat << EOF > /mnt/ec2-image/etc/default/grub
GRUB_TIMEOUT=1
GRUB_DEFAULT=saved
GRUB_DISABLE_SUBMENU=true
GRUB_TERMINAL_OUTPUT="console"
GRUB_CMDLINE_LINUX="crashkernel=auto console=ttyS0,115200n8 console=tty0"
GRUB_DISABLE_RECOVERY="true"
EOF
 
# Relabel files for selinux 
touch /mnt/ec2-image/.autorelabel
 
chroot /mnt/ec2-image grub2-install /dev/xvdf
chroot /mnt/ec2-image grub2-mkconfig -o /boot/grub2/grub.cfg
chroot /mnt/ec2-image systemctl enable lvm2-lvmetad.service
chroot /mnt/ec2-image systemctl enable lvm2-lvmetad.socket
 
yum -c /opt/ec2/yum/yum.conf --installroot=/mnt/ec2-image -y clean all
umount /mnt/ec2-image/dev/shm
umount /mnt/ec2-image/dev/pts
umount /mnt/ec2-image/dev
umount /mnt/ec2-image/sys
umount /mnt/ec2-image/proc
umount /mnt/ec2-image/boot
umount /mnt/ec2-image/var/log/audit
umount /mnt/ec2-image/var/log
umount /mnt/ec2-image/tmp
umount /mnt/ec2-image/home
umount /mnt/ec2-image/var
umount /mnt/ec2-image
 