package org.cuit.epoch.service.impl;

import cn.dev33.satoken.stp.StpUtil;
import com.alibaba.fastjson.JSON;
import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.core.toolkit.StringUtils;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import lombok.extern.slf4j.Slf4j;
import org.cuit.epoch.dto.UserDetailDTO;
import org.cuit.epoch.dto.UserOnlineDTO;
import org.cuit.epoch.entity.UserInfo;
import org.cuit.epoch.entity.UserRole;
import org.cuit.epoch.enums.FilePathEnum;
import org.cuit.epoch.exception.AppException;
import org.cuit.epoch.mapper.UserInfoMapper;
import org.cuit.epoch.service.RedisService;
import org.cuit.epoch.service.UserInfoService;
import org.cuit.epoch.service.UserRoleService;
import org.cuit.epoch.strategy.context.UploadStrategyContext;
import org.cuit.epoch.util.PageUtils;
import org.cuit.epoch.vo.*;
import org.cuit.epoch.vo.page.PageResult;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.multipart.MultipartFile;

import java.util.Comparator;
import java.util.List;
import java.util.Set;
import java.util.stream.Collectors;

import static org.cuit.epoch.enums.RedisPrefixConst.*;

/**
 * @author: ladidol
 * @date: 2022/11/20 21:31
 * @description:
 */
@Service
@Slf4j
public class UserInfoServiceImpl extends ServiceImpl<UserInfoMapper, UserInfo> implements UserInfoService {
    @Autowired
    private UserInfoMapper userInfoDao;
    @Autowired
    private UserRoleService userRoleService;
    @Autowired
    private RedisService redisService;
    @Autowired
    private UploadStrategyContext uploadStrategyContext;


    @Transactional(rollbackFor = Exception.class)
    @Override
    public void updateUserInfo(UserInfoVO userInfoVO) {
        log.info("loginId = " + StpUtil.getLoginId() + " 更改用户信息");
        // 2022/11/21 值得注意的是，需要存入redis中的序列化对象需要有构造函数。
//        UserDetailDTO userDetailDTO = (UserDetailDTO) redisService.get(USER_INFO + StpUtil.getLoginId());
        // 2022/11/22 这里就直接从session中拿用户信息吧。
        UserDetailDTO userDetailDTO = (UserDetailDTO) StpUtil.getSession().get(USER_INFO);

        // 封装用户信息
        UserInfo userInfo = UserInfo.builder()
                .id(userDetailDTO.getUserInfoId())
                .nickname(userInfoVO.getNickname())
                .intro(userInfoVO.getIntro())
                .webSite(userInfoVO.getWebSite())
                .build();
        userInfoDao.updateById(userInfo);
    }

    @Transactional(rollbackFor = Exception.class)
    @Override
    public String updateUserAvatar(MultipartFile file) {
        log.info("loginId = " + StpUtil.getLoginId() + " 更改用户头像");
        // 头像上传
        String avatar = uploadStrategyContext.executeUploadStrategy(file, FilePathEnum.AVATAR.getPath());
//        UserDetailDTO userDetailDTO = (UserDetailDTO) redisService.get(USER_INFO + StpUtil.getLoginId());
        UserDetailDTO userDetailDTO = (UserDetailDTO) StpUtil.getSession().get(USER_INFO);
        // 2022/11/22 这里就直接从session中拿用户信息吧。
        // 更新用户信息
        UserInfo userInfo = UserInfo.builder()
                .id(userDetailDTO.getUserInfoId())
                .avatar(avatar)
                .build();
        userInfoDao.updateById(userInfo);
        return avatar;
    }

    @Transactional(rollbackFor = Exception.class)
    @Override
    public void saveUserEmail(EmailVO emailVO) {
        log.info("loginId = " + StpUtil.getLoginId() + " 更改用户邮箱");
//        UserDetailDTO userDetailDTO = (UserDetailDTO) redisService.get(USER_INFO + StpUtil.getLoginId());
        UserDetailDTO userDetailDTO = (UserDetailDTO) StpUtil.getSession().get(USER_INFO);
        // 2022/11/22 这里就直接从session中拿用户信息吧。
        //依旧需要进行邮箱验证
        if (!emailVO.getCode().equals(redisService.get(USER_CODE_KEY + emailVO.getEmail()).toString())) {
            throw new AppException("验证码错误！");
        }
        UserInfo userInfo = UserInfo.builder()
                .id(userDetailDTO.getUserInfoId())
                .email(emailVO.getEmail())
                .build();
        userInfoDao.updateById(userInfo);
    }

    @Transactional(rollbackFor = Exception.class)
    @Override
    public void updateUserRole(UserRoleVO userRoleVO) {
        // 更新用户角色和昵称
        UserInfo userInfo = UserInfo.builder()
                .id(userRoleVO.getUserInfoId())
                .nickname(userRoleVO.getNickname())
                .build();
        userInfoDao.updateById(userInfo);
        // 删除用户角色重新添加
        userRoleService.remove(new LambdaQueryWrapper<UserRole>()
                .eq(UserRole::getUserId, userRoleVO.getUserInfoId()));
        List<UserRole> userRoleList = userRoleVO.getRoleIdList().stream()
                .map(roleId -> UserRole.builder()
                        .roleId(roleId)
                        .userId(userRoleVO.getUserInfoId())
                        .build())
                .collect(Collectors.toList());
        userRoleService.saveBatch(userRoleList);
    }

    @Transactional(rollbackFor = Exception.class)
    @Override
    public void updateUserDisable(UserDisableVO userDisableVO) {
        // 更新用户禁用状态
        UserInfo userInfo = UserInfo.builder()
                .id(userDisableVO.getId())
                .isDisable(userDisableVO.getIsDisable())
                .build();
        userInfoDao.updateById(userInfo);
    }

    @Transactional(rollbackFor = Exception.class)
    @Override
    public PageResult<UserOnlineDTO> listOnlineUsers(ConditionVO conditionVO) {

        // 从redis中获取全部在线用户
        Set<UserDetailDTO> onlineUser = (Set<UserDetailDTO>) redisService.get(USER_ONLINE);
        List<UserOnlineDTO> userOnlineDTOList = onlineUser.stream()
                .filter(item -> onlineUser.size() > 0)
                .map(item -> JSON.parseObject(JSON.toJSONString(item), UserOnlineDTO.class))
                .filter(item -> StringUtils.isBlank(conditionVO.getKeywords()) || item.getNickname().contains(conditionVO.getKeywords()))
                .sorted(Comparator.comparing(UserOnlineDTO::getLastLoginTime).reversed())
                .collect(Collectors.toList());

        // 执行分页
        int fromIndex = PageUtils.getLimitCurrent().intValue();
        int size = PageUtils.getSize().intValue();
        int toIndex = userOnlineDTOList.size() - fromIndex > size ? fromIndex + size : userOnlineDTOList.size();
        List<UserOnlineDTO> userOnlineList = userOnlineDTOList.subList(fromIndex, toIndex);
        return new PageResult<>(userOnlineList, userOnlineDTOList.size());
    }

    @Override
    public void removeOnlineUser(Integer userInfoId) {

        // 从redis中获取全部在线用户
        Set<UserDetailDTO> onlineUsers = (Set<UserDetailDTO>) redisService.get(USER_ONLINE);
        // 得到指定的
        List<UserDetailDTO> userInfoList = onlineUsers.stream().filter(item -> {
            UserDetailDTO userDetailDTO = (UserDetailDTO) item;
            return userDetailDTO.getUserInfoId().equals(userInfoId);
        }).collect(Collectors.toList());

        for (UserDetailDTO userDetailDTO : userInfoList) {
            log.info("踢下线： " + userDetailDTO);
            StpUtil.logout(userDetailDTO.getId());
            onlineUsers.remove(userDetailDTO);
        }
        redisService.set(USER_ONLINE, onlineUsers);

    }

}
