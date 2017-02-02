PASSWORD=$1
DOMAIN=$2
REALM=$3
ORGANIZATION=$4
DNS=$5
AMI=$6
AMIUSER=$7
IAMROLE=$8
REPO=$9
if [ "$REPO" = "default" ]; then
sudo yum install -y epel-release
else
sudo tee /etc/yum.repos.d/repos.repo > /dev/null <<EOF
[base]
name=Base
baseurl=http://$REPO/base/
gpgcheck=0
 
[updates]
name=Updates
baseurl=http://$REPO/updates/
gpgcheck=0
 
[extras]
name=Extras
baseurl=http://$REPO/extras/
gpgcheck=0

[epel]
name=EPEL
baseurl=http://$REPO/epel/
gpgcheck=0
EOF
fi
sudo yum install -y ansible
mkdir -p group_vars
# Create ansible settings file
cat > group_vars/all << EOF
image: $AMI
domain: $DOMAIN
organization: $ORGANIZATION
foreman: "foreman.{{ domain }}"
admin_password: $PASSWORD
ds_password: $PASSWORD
dns_forwarder: $DNS
realm: $REALM
ansible_ssh_private_key_file: ansible.pem
ansible_ssh_user: $AMIUSER
software_repo: $REPO
userdata: |
       #cloud-config
       hostname: {{ item.hostname }}
       fqdn: {{ item.fqdn }}
       manage_etc_hosts: true
       manage_resolv_conf: true
       resolv_conf:
         nameservers:
           - {{ dns_forwarder }}
         searchdomains:
           - {{ domain }}
         options:
           rotate: true
           timeout: 1
       puppet:
         install: false
         conf:
           agent:
             pluginsync: true
             report: true
             ignoreschedules: true
             daemon: false
             ca_server: {{ foreman }}
             certname: {{ item.fqdn }}
             environment: production
             server: {{ foreman }}
vms:
        - hostname: ipa1
          fqdn: "ipa1.{{ domain }}"
          tags:
           Name: "ipa1.{{ domain }}"
           type: ipa
           version: 7.3
          security_groups:
           - ssh
           - ipa
          public_ip: yes
          instance_type: t2.small
          role: ipa_master
          iam:
        - hostname: ipa2
          fqdn: "ipa2.{{ domain }}"
          tags:
           Name: "ipa2.{{ domain }}"
           type: ipa
           version: 7.3
          security_groups:
           - ssh
           - ipa
          public_ip: yes
          instance_type: t2.small
          role: ipa_replica
          iam:
        - hostname: foreman
          fqdn: "foreman.{{ domain }}"
          tags:
           Name: "foreman.{{ domain }}"
           type: foreman
           version: 7.3
          security_groups:
           - ssh
           - foreman
          public_ip: no
          instance_type: t2.medium
          role: foreman
          iam:
        - hostname: ansible
          fqdn: "ansible.{{ domain }}"
          tags:
           Name: "ansible.{{ domain }}"
           type: ansible
           version: 7.3
          security_groups:
           - ssh
           - prov-server
          public_ip: yes
          instance_type: t2.micro
          role: ansible
          iam: $IAMROLE
        - hostname: jump
          fqdn: "jump.{{ domain }}"
          tags:
           Name: "jump.{{ domain }}"
           type: jump
           version: 7.3
          security_groups:
           - jump
          public_ip: yes
          instance_type: t2.micro
          role: jump
          iam:                      
EOF

# Create repo templates
cat > epel.repo.j2 <<EOF
[epel]
name=Extra Packages for Enterprise Linux 7 - x86_64
baseurl=http://{{ software_repo }}/epel/
gpgcheck=0
EOF

cat > foreman.repo.j2 <<EOF
[puppetlabs-pc1]
name=Puppet Labs PC1 Repository el 7
baseurl=http://{{ software_repo }}/puppetlabs-pc1/
gpgcheck=0

[centos-sclo-sclo]
name=CentOS-7 - SCLo sclo
baseurl=http://{{ software_repo }}/centos-sclo-sclo/
gpgcheck=0

[centos-sclo-rh]
name=CentOS-7 - SCLo rh
baseurl=http://{{ software_repo }}/centos-sclo-rh/
gpgcheck=0

[epel]
name=Extra Packages for Enterprise Linux 7 - x86_64
baseurl=http://{{ software_repo }}/epel/
gpgcheck=0

[foreman-plugins]
name=Foreman plugins 1.13
baseurl=http://{{ software_repo }}/foreman-plugins/
gpgcheck=0

[foreman]
name=Foreman 1.13
baseurl=http://{{ software_repo }}/foreman/
gpgcheck=0
EOF

cat > os.repo.j2 <<EOF
[base]
name=Base
baseurl=http://{{ software_repo }}/base/
gpgcheck=0

[updates]
name=Updates
baseurl=http://{{ software_repo }}/updates/
gpgcheck=0

[extras]
name=Extras
baseurl=http://{{ software_repo }}/extras/
gpgcheck=0
EOF

# Create DNS template
cat > resolv.conf.j2 << EOF
nameserver {{ ipaserver_ip }}
EOF

# Create Hammer template
cat > cli_config.yml.j2 << EOF
:modules:
    - hammer_cli_foreman

:foreman:
    :host: 'https://{{ fqdn }}/'
    :username: 'admin'
    :password: '{{ admin_password }}'

:log_dir: '~/.foreman/log'
:log_level: 'error'
EOF

# Create provision-server configuration template
cat > prov-server.toml.j2 << EOF
[aws]
region = "{{ region }}"

[ansible]
inventory = "/var/lib/prov-server/inventory"
playbook = "/var/lib/prov-server/host.yml"

[user]
name = "{{ ansible_ssh_user }}"
key = "{{ prov_key.stdout }}"
EOF

# Create provision-server ansible variable template
cat > all.j2 << EOF
---
{% raw %}
pass: '{{ lookup(''password'', ''/tmp/'' + fqdn ) }}'
{% endraw %}
realm: {{ realm }}
domain: {{ domain }}
ansible_ssh_user: {{ ansible_ssh_user }}
ipa1: {{ groups.ipa_master[0] }}
ipa2: {{ groups.ipa_replica[0] }}
hostgroup: infrastructure
environment: production
puppet_master: foreman.{{ domain }}
organization: {{ organization }}
location: {{ region }}
EOF

# Create ansible playbook
cat > start.yml << EOF
---
- hosts: localhost
  connection: local
  tasks:
   - name: Get AWS Information
     ec2_facts:

   - name: Set Network Variables
     set_fact:
      vpc: "{{ facter_ec2_metadata.network.interfaces.macs[facter_ec2_metadata.mac]['vpc-id'] }}"
      cidr: "{{ facter_ec2_metadata.network.interfaces.macs[facter_ec2_metadata.mac]['vpc-ipv4-cidr-block'] }}"
      subnet: "{{ facter_ec2_metadata.network.interfaces.macs[facter_ec2_metadata.mac]['subnet-id'] }}"
      region: "{{ ansible_ec2_placement_region }}"

   - name: Create a new keypair
     ec2_key:
        name: ansible
        region: "{{ region }}"
     register: keypair

   - name: Save private key
     copy:
        dest: ./ansible.pem
        content: "{{ keypair.key.private_key }}"
        mode: 0600
     when: keypair.changed

   - name: Create Jump Server Security Group
     ec2_group:
        name: jump
        description: Allow SSH from anywhere
        vpc_id: "{{ vpc }}"
        region: "{{ region }}"
        rules:
          - proto: tcp
            from_port: 22
            to_port: 22
            cidr_ip: 0.0.0.0/0

   - name: Create SSH Server Security Group
     ec2_group:
        name: ssh
        description: Allow SSH within VPC
        vpc_id: "{{ vpc }}"
        region: "{{ region }}"
        rules:
          - proto: tcp
            from_port: 22
            to_port: 22
            cidr_ip: "{{ cidr }}"

   - name: Create Provision Server Security Group
     ec2_group:
        name: prov-server
        description: Rules for Provisioning servers
        vpc_id: "{{ vpc }}"
        region: "{{ region }}"
        rules:
          - proto: tcp
            from_port: 8080
            to_port: 8080
            cidr_ip: "{{ cidr }}"

   - name: Create Foreman Server Security Group
     ec2_group:
        name: foreman
        description: Rules for Foreman servers
        vpc_id: "{{ vpc }}"
        region: "{{ region }}"
        rules:
          - proto: tcp
            from_port: 80
            to_port: 80
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 443
            to_port: 443
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 8443
            to_port: 8443
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 8140
            to_port: 8140
            cidr_ip: "{{ cidr }}"

   - name: Create IPA Security Group
     ec2_group:
        name: ipa
        description: Rules for IPA directory servers
        vpc_id: "{{ vpc }}"
        region: "{{ region }}"
        rules:
          - proto: tcp
            from_port: 80
            to_port: 80
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 464
            to_port: 464
            cidr_ip: "{{ cidr }}"
          - proto: udp
            from_port: 464
            to_port: 464
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 636
            to_port: 636
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 53
            to_port: 53
            cidr_ip: "{{ cidr }}"
          - proto: udp
            from_port: 53
            to_port: 53
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 389
            to_port: 389
            cidr_ip: "{{ cidr }}"
          - proto: udp
            from_port: 123
            to_port: 123
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 443
            to_port: 443
            cidr_ip: "{{ cidr }}"
          - proto: tcp
            from_port: 88
            to_port: 88
            cidr_ip: "{{ cidr }}"
          - proto: udp
            from_port: 88
            to_port: 88
            cidr_ip: "{{ cidr }}"

   - name: Start VMs
     with_items: "{{ vms }}"
     ec2:
        exact_count: 1
        count_tag: "{{ item.tags }}"
        instance_type: "{{ item.instance_type }}"
        region: "{{ region }}"
        assign_public_ip: "{{ item.public_ip }}"
        group: "{{ item.security_groups }}"
        key_name: ansible
        user_data: "{{ userdata }}"
        instance_tags: "{{ item.tags }}"
        wait: yes
        vpc_subnet_id: "{{ subnet }}"
        image: "{{ image }}"
        instance_profile_name: "{{ item.iam }}"
     register: instances

   - add_host:
      hostname: "{{ item.tagged_instances[0].private_ip }}"
      groups: "{{ item.item.role }}"
      fqdn: "{{ item.item.fqdn}}"
      region: "{{ region }}"
     with_items: "{{ instances.results }}"

- name: Install IPA
  hosts:
   - ipa_master
   - ipa_replica
  gather_facts: false
  tasks:
   - name: Wait for SSH
     local_action:
      module: wait_for
      port: 22
      host: "{{ inventory_hostname }}"
      search_regex: OpenSSH
      delay: 10
      timeout: 3600

   - name: Sleep Waiting for SSH Keygen
     pause:
       seconds: 60

   - name: Configure OS repos
     when: software_repo != 'default'
     become: yes
     template:
       src: os.repo.j2
       dest: /etc/yum.repos.d/os.repo

   - name: Install IPA
     become: yes
     yum: name={{ item }} state=present
     with_items:
      - ipa-server
      - ipa-server-dns
   - name: Open Firewall
     become: yes
     firewalld: service={{ item }} permanent=true state=enabled immediate=true
     with_items:
      - http
      - https
      - ldap
      - ldaps
      - kerberos
      - kpasswd
      - ntp
      - dns

- name: Configure IPA Master
  hosts:
   - ipa_master
  tasks:
   - name: Run Installer
     become: yes
     command: ipa-server-install --mkhomedir --setup-dns --forwarder={{ dns_forwarder }} --unattended --ds-password={{ ds_password }} --hostname={{ fqdn }} --admin-password={{ admin_password }} --realm={{ realm }} --ip-address={{ inventory_hostname }}
     args:
       creates: /etc/ipa/default.conf

- name: Configure IPA clients
  hosts:
   - ipa_replica
   - foreman
   - ansible
   - jump
  vars:
   ipaserver_ip: "{{ groups.ipa_master[0] }}"
  tasks:

   - name: Set hostname
     become: yes
     hostname:
       name: "{{ fqdn }}"

   - name: Set IPA as DNS
     become: yes
     template:
       src: resolv.conf.j2
       dest: /etc/resolv.conf

   - name: Configure ipa client
     become: yes
     command: ipa-client-install --mkhomedir --principal=admin --password={{ admin_password }} --unattended
     args:
       creates: /etc/ipa/default.conf

- name: Configure IPA Replica
  hosts:
   - ipa_replica
  tasks:
   - name: Configure replica
     become: yes
     command: ipa-replica-install --setup-ca --setup-dns --forwarder={{ dns_forwarder }} --unattended --admin-password={{ admin_password }} --mkhomedir
     args:
       creates: /etc/systemd/system/multi-user.target.wants/ipa.service

- hosts: foreman
  gather_facts: false
  tasks:
   - name: Install Foreman Repos
     become: yes
     when: software_repo == 'default'
     yum: name={{ item }} state=present
     with_items:
      - https://yum.puppetlabs.com/puppetlabs-release-pc1-el-7.noarch.rpm
      - epel-release
      - https://yum.theforeman.org/releases/1.13/el7/x86_64/foreman-release.rpm

   - name: Configure OS Repos
     become: yes
     when: software_repo != 'default'
     template:
       src: os.repo.j2
       dest: /etc/yum.repos.d/os.repo

   - name: Configure Foreman Repos
     become: yes
     when: software_repo != 'default'
     template:
       src: foreman.repo.j2
       dest: /etc/yum.repos.d/foreman.repo

   - name: Install Foreman Installer
     become: yes
     yum: name={{ item }} state=present
     with_items:
      - foreman-installer
      - ipa-admintools
      - httpd

   - name: Trust IPA certificates
     become: yes
     copy:
      remote_src: True
      src: /etc/ipa/ca.crt
      dest: /etc/pki/ca-trust/source/anchors/ca.crt
     register: copy

   - name: Update Trust
     become: yes
     command: update-ca-trust
     when: copy.changed

   - name: Create HTTP Service Principal
     become: yes
     shell: echo "{{ admin_password }}" | kinit admin && ipa service-add "HTTP/{{ fqdn }}" && ipa-getkeytab -k /etc/http.keytab -p "HTTP/{{ fqdn }}"
     args:
      creates: /etc/http.keytab

   - name: Set keytab permissions
     become: yes
     file:
       path: /etc/http.keytab
       owner: apache
       group: apache
       mode: 600

   - name: Open Firewall Services
     become: yes
     firewalld: service={{ item }} permanent=true state=enabled immediate=true
     with_items:
      - http
      - https

   - name: Open Firewall Ports
     become: yes
     firewalld: port={{ item }} permanent=true state=enabled immediate=true
     with_items:
      - 8140/tcp
      - 8443/tcp

   - name: Set Default Foreman Options
     set_fact:
      foreman_opts: ''
     when: software_repo == 'default'

   - name: Set Extra Foreman Options
     set_fact:
      foreman_opts: '--foreman-configure-epel-repo=false --foreman-configure-scl-repo=false'
     when: software_repo != 'default'

   - name: Install Foreman
     become: yes
     shell: foreman-installer --foreman-admin-password {{ admin_password }} --enable-foreman-proxy-plugin-openscap --enable-foreman-plugin-openscap --foreman-ipa-authentication=true --foreman-organizations-enabled=true --foreman-locations-enabled=true --foreman-initial-location={{ region }} --foreman-initial-organization={{ organization }} --enable-foreman-proxy {{ foreman_opts}}  && touch /etc/foreman/.installed
     environment:
      LANG: "en_US.UTF-8"
      LC_ALL: "en_US.UTF-8"
     args:
      creates: /etc/foreman/.installed

   - name: Create realm-proxy
     become: yes
     shell: echo -n "{{ admin_password }}" | foreman-prepare-realm admin realm-proxy && cp freeipa.keytab /etc/foreman-proxy/ && rm -f freeipa.keytab && chown foreman-proxy /etc/foreman-proxy/freeipa.keytab && chmod 600 /etc/foreman-proxy/freeipa.keytab
     args:
      creates: /etc/foreman-proxy/freeipa.keytab

   - name: Copy keytab for use by prov-server
     become: yes
     copy:
       remote_src: True
       src: /etc/foreman-proxy/freeipa.keytab
       dest: "/home/{{ ansible_ssh_user }}/freeipa.keytab"
       owner: "{{ ansible_ssh_user }}"
       group: "{{ ansible_ssh_user }}"
       mode: 0600

   - name: Check realm status
     become: yes
     command: 'grep -c "enabled: false" /etc/foreman-proxy/settings.d/realm.yml'
     changed_when: False
     failed_when: False
     register: realm_check

   - name: Enable realm
     become: yes
     shell: foreman-installer --foreman-proxy-realm-principal=realm-proxy@{{ realm }} --foreman-proxy-realm=true
     environment:
      LANG: "en_US.UTF-8"
      LC_ALL: "en_US.UTF-8"
     when: realm_check.stdout_lines[0] == '1'

   - name: Make Hammer Settings directory
     file:
       path: "/home/{{ ansible_ssh_user }}/.hammer"
       state: directory

   - name: Configure Hammer
     template:
       src: cli_config.yml.j2
       dest: "/home/{{ ansible_ssh_user }}/.hammer/cli_config.yml"

   - name: Check for realm
     shell: hammer realm list | grep -c {{ realm }}
     changed_when: False
     failed_when: False
     register: realm_check

   - name: Create realm
     command: hammer realm create --name {{ realm }} --realm-type FreeIPA --organizations {{ organization }} --locations {{ region }} --realm-proxy-id 1
     when: realm_check.stdout_lines[0] == '0'

   - name: Check for environment
     shell: hammer environment list | grep -c production
     changed_when: False
     failed_when: False
     register: env_check

   - name: Create environment
     command: hammer environment create --name production  --organizations {{ organization }} --locations {{ region }}
     when: env_check.stdout_lines[0] == '0'

   - name: Check for hostgroup
     shell: hammer hostgroup list | grep -c infrastructure
     changed_when: False
     failed_when: False
     register: hostgroup_check

   - name: Create hostgroup
     command: hammer hostgroup create --name infrastructure --environment production --puppet-ca-proxy {{ fqdn }} --puppet-proxy {{ fqdn }} --organizations {{ organization }} --locations {{ region }}
     when: hostgroup_check.stdout_lines[0] == '0'

   - name: Check for domain
     shell: hammer domain list | grep -c {{ domain }}
     changed_when: False
     failed_when: False
     register: domain_check

   - name: Create domain
     command: hammer domain create --name {{ domain }}  --organizations {{ organization }} --locations {{ region }}
     when: domain_check.stdout_lines[0] == '0'

   - name: Install Puppet Modules
     become: yes
     when: software_repo == 'default'
     command: /opt/puppetlabs/bin/puppet module install -i /etc/puppetlabs/code/environments/production/modules {{ item }}
     with_items:
      - puppetlabs/ntp
      - wdijkerman/zabbix
      - saz/resolv_conf
      - jlambert121-yum
      - treydock-yum_cron
      - isimluk-foreman_scap_client
     register: puppet
     args:
      creates: /etc/puppetlabs/code/environments/production/modules/foreman_scap_client/manifests/init.pp

   - name: Download Module Archive
     become: yes
     when: software_repo != 'default'
     register: puppet
     unarchive:
      src: "http://{{ software_repo }}/puppet.tgz"
      remote_src: true
      dest: /etc/puppetlabs/code/environments/production/modules
      creates: /etc/puppetlabs/code/environments/production/modules/foreman_scap_client/manifests/init.pp

   - name: Import Puppet Classes
     command: hammer proxy import-classes --id 1
     when: puppet.changed

- name: Configure Provision Server
  hosts: ansible
  gather_facts: false
  tasks:

   - name: Configure OS Repos
     become: yes
     when: software_repo != 'default'
     template:
       src: os.repo.j2
       dest: /etc/yum.repos.d/os.repo

   - name: Configure EPEL Repos
     become: yes
     when: software_repo != 'default'
     template:
       src: epel.repo.j2
       dest: /etc/yum.repos.d/epel.repo

   - name: Install EPEL Repo
     become: yes
     when: software_repo == 'default'
     yum: name=epel-release state=present

   - name: Install Ansible
     become: yes
     yum: name=ansible state=present

   - name: Copy Provision Server RPM
     copy:
       src: /tmp/prov-server.rpm
       dest: /tmp/prov-server.rpm

   - name: Install Provision Server
     become: yes
     yum: name={{ item }} state=present
     with_items:
      - /tmp/prov-server.rpm

   - name: Open Firewall Ports
     become: yes
     firewalld: port={{ item }} permanent=true state=enabled immediate=true
     with_items:
      - 8080/tcp

   - name: Generate SSH key
     become: yes
     become_user: prov-server
     shell: ssh-keygen -b 2048 -t rsa -f /var/lib/prov-server/.ssh/id_rsa -q -N ""
     args:
      creates: /var/lib/prov-server/.ssh/id_rsa

   - name: Capture Public Key
     become: yes
     command: cat /var/lib/prov-server/.ssh/id_rsa.pub
     register: prov_key

   - name: Configure Provision Server
     become: yes
     template:
       src: prov-server.toml.j2
       dest: /etc/provision/prov-server.toml
   - name: Set provisioning Variables
     become: yes
     template:
       src: all.j2
       dest: /var/lib/prov-server/group_vars/all

   - name: Start and Enable Server
     become: yes
     service:
      name: prov-server
      state: started
      enabled: yes

- name: Install and Provision Client
  hosts:
   - foreman
   - ipa_master
   - ipa_replica
   - jump
   - ansible
  gather_facts: false
  tasks:
   - name: Copy Provision Client RPM
     copy:
       src: /tmp/prov-client.rpm
       dest: /tmp/prov-client.rpm
   - name: Install Provision Client
     become: yes
     yum: name={{ item }} state=present
     with_items:
      - /tmp/prov-client.rpm
   - name: Run Provision Client
     become: yes
     command: prov-client -ip {{ groups.ansible[0] }}

- name: Dump out private key
  hosts: ansible
  tasks:
   - name: Capture Key
     become: yes
     command: cat /var/lib/prov-server/.ssh/id_rsa
     register: prov_key
   - debug: var=prov_key.stdout_lines                           
EOF
export ANSIBLE_HOST_KEY_CHECKING=False
ansible-playbook start.yml