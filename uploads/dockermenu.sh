#/bin/bash



 
ConfigureRailsContainer(){
# Ensure chosen container is started
    StartContainer
    docker exec -it $ContainerName sh -c " apt update; apt upgrade -y; apt install -y git curl bundler libsqlite3-dev make g++ ruby-dev apt-utils yarn unixodbc unixodbc-dev bash-completion dialog nano;  gem install rails tzinfo-data dbi dbd-odbc ruby-odbc ruby-oci8 activerecord-odbc-adapter;"
}


UpdateUpgradeContainer(){
# Ensure chosen container is started
    StartContainer
# Send commands to chosen container
    docker exec -it $ContainerName sh -c " apt update; apt upgrade -y;  "
}
 

PushPullProject(){

# tell this version of git some default behaviour
      git config --global push.default simple
    git config user.email "some@developer.com"
    git config user.name "Docker_Menu-APP"

# update the Projects folder

    git pull
    git add /rubydatahandler/projects/.
    git commit -m 'Push & Pull Ruby Projects'
    git push
}

 

# docker commit b02ccc19d8d6 filemanager

# run -it --name fima -v /5p4c3/fmdata:/

 

 

# V ----------------------- Variables & Arrays-------------------

RED='\033[0;41;30m'
STD='\033[0;0;39m'
DateTime=$(date)

pause(){

# Require user interaction to proceed
  read -p "Press [Enter] key to continue..." fackEnterKey
}

# As Root, add desired user to Docker group:  usermod -aG docker ${USER}
# As Root,  sed

# modify files to be executable:  chmod +sx *.*

# Delete folder and files within:  rm -r -f consumerdevenv/

 

#################################################################

# ------------ Data Capture -----------

CaptureImageName(){

# Display all existing container info

    FullContList

# capture existing image name   

   read -rp "Enter an image name: " ImageName

    if [ $ImageName="" ];

    then

# Complain about the lack of image name and stop

    echo "Please select an image...."   

    else

# Display the selected image.

    echo "Using:" $ImageName   

    fi   

}

 

DefineContName(){

# Capture and pass container name as name for new host
    read -rp "Enter the Container Name: " ContainerName
    ContName="--name $ContainerName"
}

NameContainer(){
# Capture and validate the new desired Container Name
    read -rp "Enter the Container Name: " ContainerName
    if [ $ContainerName == "" ];
    then
# Complain about the lack of image name and stop
    echo "Please enter a container name...."   
    else
# Display the selected image.
    echo "You Entered: "$ContainerName   
    fi   
}  

 

RenameContainerOld(){
# Rename a container
# Capture desired Container Name
    read -rp "Enter the current container Name: " ContainerName
# Capture desired Container Name
    read -rp "Enter the new container Name: " NewContainerName   
    docker rename $ContainerName $NewContainerName
}

 

RenameContainer(){

# Capture image name to change  

    CaptureImageName

# capture and validate the new image name   

   read -rp "Enter the new container name: " new_container

    if [ $new_container="" ];

    then

# Complain about the lack of image name and stop

    echo "Please enter a new image name...."   

    else

# Display the selected image.

    echo "replacing: " $ImageName "with:" $new_container   

    fi   

# Rename container

    docker rename $ImageName $new_container

}

 

DefineOS(){

# "Capture desired Operating System"

    read -rp "Enter your desired Operating System: " opesys

}

 

DefineSW(){

# "Capture Software requirements "

    read -rp "Enter your Software requirements: " reqsof

}


UserInContainer(){
# Capture desired username for use within container
    read -rp "Enter the User Name: " LoginUser

}

 

# ------Construct-----

BuildLocal(){

# Build from local container

    read -rp "Enter file location or leave blank for current location: " BuildFile

    if [ $BuildFile="" ];

    then docker-compose up --build -d

    else cd $BuildFile docker-compose up --build -d

    fi

}

 

StartContainer(){
# Start a container and leave it running.
    docker start $ContainerName 
}

 

BasicCon(){

# create Ubuntu20.04 container with only apt and bash from source

    docker create --name $ContainerName 

}

DefinedBuild(){

# "Create a container with the desired Operating System & software, Allocate a pseudo-TTY and Attach to STDIN, STDOUT or STDERR. Operating System and required Software are defined as needed"
    docker create -t -i --name $ContainerName $opesys $reqsof
    # output ID to be captured
}

 

PullImage(){

# Pull a specific image for use

# Capture user input, if nothing entered default to ubuntu 20.04

    echo "view images at: https://registry.docker.nat.bt.com/harbor/projects"

    read -rp "Enter the desired Image (eg. ubuntu-focal:latest) or leave blank for 20.04: " TheImage

# Validate user entered some data.   

    if [[ $TheImage == "" ]]

# If TheImage variable has no data (null) then assign it to this:   

    then TheImage="ubuntu-focal:latest"

# Then pull the above image

    docker pull $TheImage

# Should $TheImage variable contains some data, pull that image instead.   

    else

#  pull desired image   

    docker pull $TheImage

# Close this if loop.   

    fi

}

 

buildthis(){

 

# "Create a container with the desired Operating System & software, Allocate a pseudo-TTY and Attach to STDIN, STDOUT or STDERR. Operating System and required Software are defined as needed"

    docker create -t -i $opesys $reqsof

 

    # output ID to be captured

}

 

SaveTar(){

#Create a backup that can then be used with docker load.

 

    mkdir -v /5p4c3/Images/

# execute command with variables

    docker save -o /5p4c3/Images/$ContainerName.tar $ImageName

# Display created file

    ls -sh /5p4c3/Images/$ContainerName.tar

 

# commit an existing container to a new image:

# docker commit b02ccc19d8d6 filemanager

# Create container from created image:

# docker run -it --name fima -v /5p4c3/fmdata:/fmdata filemanager

 

}

 

LoadTar(){

# load a saved tarball

    docker load -i /5p4c3/Images/$ContainerName.tar

 

}

 

DeleteContainer(){

# Delete an existing Container

docker rm $ContainerName

}

 

RunCon(){

# start the container and attach to STDIN, STDOUT or STDERR

    docker start -a -i $ContainerName

}

 

StopCon(){

    docker stop $ContainerName

}

# show all existing container info

FullContList(){

# capture image name   

# List all containers by ID

   docker ps -a #--no-trunc

}

 

ContList(){
# show most relevant info
    docker inspect -f '{{.Name}} - {{.State.Running}} - {{.NetworkSettings.IPAddress }}' $(docker ps -aq)
}


RunConProxy(){
# start the container and attach to STDIN, STDOUT or STDERR
    docker start -a -i $ContainerName  HTTP_PROXY=""
}


# --------------------- Container Interaction ---------------------

 

Acommand(){

# Capture desired command for container or provide CLI if no data recieved.

    read -rp "Enter the desired command or leave blank to quit: " DockerCommand

    if [[ $DockerCommand == "" ]]

    then

    echo "No command recieved, /bin/bash"

    DockerCommand="/bin/bash"

    else

    echo $DockerCommand

    fi

}

 

IssueCommand(){

# execute a command in named container

    docker exec -it $ContainerName sh -c "$DockerCommand"

# Run another command on the same container

    Acommand

}

 

ContainerLogin(){

# Login to an existing Container as root

    docker container attach $ContainerName

}


UserLogin(){

# Login to an existing Container with the specified user and run a shell

    docker exec -it --user $LoginUser $ContainerName /bin/bash

}


#   -t, --tty                            Allocate a pseudo-TTY
#   -a, --attach list                    Attach to STDIN, STDOUT or STDERR
#   -i, --interactive                    Keep STDIN open even if not attached


BasicDockerCon(){

# create container with only apt and bash

    docker create -t -i $ContName ubuntu

}

 

AddProxy(){
# start the container
    docker start $ContainerName
}


CopyFolder2Host(){

# Copy a folder from a container to a git folder on the host

# Create git initialised folder on the host

# An example: docker cp reverent_knuth:/usr/local/SmartBear/SoapUI-5.6.0/bin/regression-testing-fire/FIRE_Offers_Service-BT_FIRE_REGRESSION_SUITE-TEST_FIRE_REGRESSION_UFAST01_1-0-OK.txt ~/testdata

# Define location in container to copy from

    read -rp "Define location in container to copy from: " CopyFrom

    if [[ $CopyFrom == "" ]]

    then echo "A location is required."

    else echo $CopyFrom

    fi

# Define location on host to copy to.

    read -rp "Define location on host to copy to: " CopyTo

    if [[ $CopyTo == "" ]]

    then echo "A location is required."

    else echo $CopyTo

    fi

# Execute command

    docker cp $ContainerName:$CopyFrom $CopyTo

# display source files to be copied

    ls $CopyFrom

# Show copied files in destination

    ls $CopyTo

}

 

BackupFMdata(){

    docker cp ruby:/fmdata/ /5p4c3/

    ls /5p4c3/fmdata/

}

 

RunTestTools(){

    docker start ruby

    sleep 2

    docker exec -w /fmdata/ -d ruby ruby /soapuiprojectdata/BasicSinatraApp.rb

}

 

RunFileManager(){

# Start File Manager and detach

  #  cd /5p4c3

    docker start fima

    sleep 2

  #  docker cp ruby:/fmdata/ /5p4c3/fmdata

    docker exec -w /fmdata/ -d fima ruby /soapuiprojectdata/FileManager.rb --no-auth --no-timeout --version-uploads

}

 

Viewfmdata(){

    echo -e "${RED} contents of fmdata folder: ${STD}"

    ls /5p4c3/fmdata/

}

 

UpdateContainers(){

    docker exec -d fima /soapuiprojectdata/SinatraMenu.sh UpdateSoftware

    docker exec -d ruby /soapuiprojectdata/SinatraMenu.sh UpdateSoftware

#    docker stop fima

#    docker stop ruby

}

 

UpdateRuby(){

# Called externally, this allows for an update of this repo on the desired container to be performed.

    docker exec -d ruby /soapuiprojectdata/SinatraMenu.sh UpdateSoftware

}

RemoveDocker(){
for pkg in docker.io docker-doc docker-compose docker-compose-v2 podman-docker containerd runc; do sudo apt-get remove $pkg; done
}


DockerInstall(){

# Add Docker's official GPG key:
sudo apt-get update
sudo apt-get install ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc

# Add the repository to Apt sources:
echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update
sudo apt-get install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

}

#----------------------------------------------------------


# this handles displaying menu items and there corresponding commands

DockerMenu(){  

    docker_menu(){

# This displays the desired menu items and selection number.

    echo ""
    echo " Docker MENU"
    echo " Common Docker commands in a simple menu. "
    echo "------------------------"
    echo "1.  Setup Dockers Requirements via Snap"
    echo "2.  Show more container info. "
#    echo "3.  Create a Basic Container (Broke use opt.50) Provide BASH in an Ubuntu container "
    echo "4.  Define a new Container        Define, Start & Login to a Container "
    echo "5.  Start Container & connect     Choose which container to start "
    echo "6.  Stop Container                Choose which container to stop "
    echo "7.  Login to a running Container "
    echo "8.  build local image"
    echo "9.  Start an existing container. "
#    echo "10. Add proxies to Container "
    echo "11.  "
    echo "20. Issue a command to a container"
    echo "21. Run FileManager within fima Container in background."
    echo "22. Run TestTools within ruby Container in background. "
    echo "23. Start Filemanager and Testtools."
    echo "27. Select, update, upgrade and install software for container prep. "
    echo "28. Update fima & ruby Containers. "
    echo "29. Update the Ruby container"
    echo "30. Pull a Docker image"
#    echo "33. Developing "
    echo "34. Add RoR to an existing container"
    echo "36. Git Push/Pull project Changes"
    echo "40. Save a container image"
    echo "41. Load a container image"
    echo "42. Rename a container "
    echo "43. Copy files/folder to the host"
#   echo "44. View the fmdata folder upon the Host."
#    echo "45. Backup fmdata to 5p4c3 folder."
    echo "50. Create Docker Ubuntu Container"
#    echo "51. Add a proxy to a container "
#   echo "52. Run container with Proxy Switch"
    echo "53. Create a RoR container"
#    echo "60. Login with chosen user and container "
    echo "91. Delete a Container"  
    echo "95.  "
    echo "99. Exit "

    echo "------------------------"

    ContList

}

 

docker_options(){

# The commads (or code blocks) which are to be executed. linked to the displayed options above via matching characters.

    local choice

    read -p "Enter choice [ 1 - 99] " choice

    case $choice in

        1)  sudo apt install docker.io docker-compose && sudo usermod -aG docker $USER && su - $USER ;;
        2)  FullContList ;;   
        3)  NameContainer && BasicCon && SetProxy && RunCon ;;
        4)  NameContainer && DefineOS && DefineSW && DefinedBuild && RunCon ;;
        5)  NameContainer && RunCon ;;
        6)  NameContainer && StopCon ;;
        7)  NameContainer && ContainerLogin ;;
        8)  BuildLocal ;;
        9)  NameContainer && StartContainer ;;
        10) DefineContName && StartContainer && SetProxy ;;
        20) NameContainer && StartContainer && Acommand && IssueCommand ;;
        21) RunFileManager ;;
        22) RunTestTools ;;
        23) RunTestTools && RunFileManager ;;
 
        27) NameContainer && UpdateUpgradeContainer ;;
        28) UpdateContainers ;;
        29) UpdateRuby ;;
        30) PullImage ;;
        33) Developing ;;
        34) NameContainer && ConfigureRailsContainer ;;
        36) PushPullProject ;;
        40) NameContainer && CaptureImageName && SaveTar ;;
        41) NameContainer && LoadTar ;;
        42) RenameContainer ;;
        43) NameContainer && CopyFolder2Host ;;
        44) Viewfmdata ;;
        45) BackupFMdata ;;

        50) DefineContName && BasicDockerCon && AddProxy ;;
        51) NameContainer && AddProxy ;;
        52) NameContainer && RunConProxy ;;
        53) NameContainer && BasicDockerCon && ConfigureRailsContainer ;;
 

        60) UserInContainer && NameContainer && RunCon && UserLogin ;;
        91) NameContainer && StopCon && DeleteContainer ;;
        95)  ;;
        99) clear && echo "Goodbye $USER" && exit ;;
        *) echo -e "${RED} sending $choice to bin/bash.....${STD}" && echo -e $choice | /bin/bash
    esac
}

while true
do
    docker_menu
    docker_options
done

}

# At startup, DockerMenu displays the menu if no switch added.

#
    if [ $1 == ""]
    then RunModule="DockerMenu"
    else RunModule=$1
    echo "$1"
    fi
    $RunModule
 

#   List of available commands to run.
#   DockerMenu
#   BasicCon
#   NameContainer
#   DefineOS
#   DefineSW
#   DefinedBuild
#   RunCon
#   StopCon
#   ContList   



# lsof -iTCP -P -n

# rails s -b 0.0.0.0 -d

 

 

 